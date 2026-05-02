package grpc

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/netbirdio/netbird/flow/proto"
	"github.com/netbirdio/netbird/management/internals/modules/networktraffic"
	"github.com/netbirdio/netbird/management/server/account"
	nbpeer "github.com/netbirdio/netbird/management/server/peer"
	"github.com/netbirdio/netbird/management/server/store"
)

type FlowServer struct {
	proto.UnimplementedFlowServiceServer
	accountManager account.Manager
}

func NewFlowServer(accountManager account.Manager) *FlowServer {
	return &FlowServer{accountManager: accountManager}
}

func (s *FlowServer) Events(stream proto.FlowService_EventsServer) error {
	initiator, err := stream.Recv()
	if err != nil {
		return err
	}
	if !initiator.GetIsInitiator() {
		return status.Error(codes.InvalidArgument, "missing initiator frame")
	}
	if err := stream.Send(&proto.FlowEventAck{IsInitiator: true}); err != nil {
		return err
	}

	for {
		event, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		if saveErr := s.saveEvent(stream.Context(), event); saveErr != nil {
			log.WithContext(stream.Context()).Warnf("failed to persist flow event: %v", saveErr)
			if isPermanentFlowError(saveErr) {
				if ackErr := stream.Send(&proto.FlowEventAck{EventId: event.GetEventId()}); ackErr != nil {
					return ackErr
				}
			}
			continue
		}

		if err := stream.Send(&proto.FlowEventAck{EventId: event.GetEventId()}); err != nil {
			return err
		}
	}
}

func (s *FlowServer) saveEvent(ctx context.Context, event *proto.FlowEvent) error {
	peerKey, err := wgtypes.NewKey(event.GetPublicKey())
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	reporter, err := s.accountManager.GetStore().GetPeerByPeerPubKey(ctx, store.LockingStrengthNone, peerKey.String())
	if err != nil {
		return fmt.Errorf("resolve reporter peer: %w", err)
	}

	fields := event.GetFlowFields()
	if fields == nil {
		return errors.New("flow fields are empty")
	}

	var sourcePort uint32
	var destPort uint32
	var icmpType uint32
	var icmpCode uint32
	if portInfo := fields.GetPortInfo(); portInfo != nil {
		sourcePort = portInfo.GetSourcePort()
		destPort = portInfo.GetDestPort()
	}
	if icmpInfo := fields.GetIcmpInfo(); icmpInfo != nil {
		icmpType = icmpInfo.GetIcmpType()
		icmpCode = icmpInfo.GetIcmpCode()
	}

	sourceIP := net.IP(fields.GetSourceIp())
	destIP := net.IP(fields.GetDestIp())

	source, err := s.resolveEndpoint(ctx, reporter.AccountID, sourceIP, sourcePort, string(fields.GetSourceResourceId()))
	if err != nil {
		return err
	}
	destination, err := s.resolveEndpoint(ctx, reporter.AccountID, destIP, destPort, string(fields.GetDestResourceId()))
	if err != nil {
		return err
	}

	userName := ""
	userEmail := ""
	if reporter.UserID != "" {
		user, userErr := s.accountManager.GetStore().GetUserByUserID(ctx, store.LockingStrengthNone, reporter.UserID)
		if userErr == nil && user != nil {
			userName = user.Name
			userEmail = user.Email
		}
	}

	connectionType := networktraffic.ConnectionTypeP2P
	if source.Type != networktraffic.EndpointTypePeer || destination.Type != networktraffic.EndpointTypePeer {
		connectionType = networktraffic.ConnectionTypeRouted
	}

	timestamp := event.GetTimestamp().AsTime().UTC()
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	record := &networktraffic.Event{
		ID:                     base64.RawURLEncoding.EncodeToString(event.GetEventId()),
		AccountID:              reporter.AccountID,
		FlowID:                 base64.RawURLEncoding.EncodeToString(fields.GetFlowId()),
		Timestamp:              timestamp,
		EventType:              fields.GetType().String(),
		Direction:              fields.GetDirection().String(),
		Protocol:               int(fields.GetProtocol()),
		ConnectionType:         connectionType,
		ReporterID:             reporter.ID,
		UserID:                 reporter.UserID,
		UserName:               userName,
		UserEmail:              userEmail,
		SourceID:               source.ID,
		SourceType:             source.Type,
		SourceName:             source.Name,
		SourceAddress:          source.Address,
		SourceDNSLabel:         source.DNSLabel,
		SourceOS:               source.OS,
		SourceCountryCode:      source.CountryCode,
		SourceCityName:         source.CityName,
		DestinationID:          destination.ID,
		DestinationType:        destination.Type,
		DestinationName:        destination.Name,
		DestinationAddress:     destination.Address,
		DestinationDNSLabel:    destination.DNSLabel,
		DestinationOS:          destination.OS,
		DestinationCountryCode: destination.CountryCode,
		DestinationCityName:    destination.CityName,
		ICMPType:               int(icmpType),
		ICMPCode:               int(icmpCode),
		RxBytes:                int64(fields.GetRxBytes()),
		RxPackets:              int64(fields.GetRxPackets()),
		TxBytes:                int64(fields.GetTxBytes()),
		TxPackets:              int64(fields.GetTxPackets()),
	}

	if dnsInfo := fields.GetDnsInfo(); dnsInfo != nil {
		record.DNSDomain = dnsInfo.GetDomain()
		record.DNSQueryType = dnsInfo.GetQueryType()
		record.DNSAnswers = dnsInfo.GetAnswers()
		record.DNSRCode = dnsInfo.GetRcode()
	}

	return s.accountManager.GetStore().CreateNetworkTrafficEvent(ctx, record)
}

type resolvedEndpoint struct {
	ID          string
	Type        string
	Name        string
	Address     string
	DNSLabel    string
	OS          string
	CountryCode string
	CityName    string
}

func (s *FlowServer) resolveEndpoint(ctx context.Context, accountID string, ip net.IP, port uint32, resourceID string) (*resolvedEndpoint, error) {
	address := networktraffic.FormatAddress(ip, port)

	if resourceID != "" {
		resource, err := s.accountManager.GetStore().GetNetworkResourceByID(ctx, store.LockingStrengthNone, accountID, resourceID)
		if err == nil && resource != nil {
			return &resolvedEndpoint{
				ID:      resource.ID,
				Type:    networktraffic.EndpointTypeHostResource,
				Name:    resource.Name,
				Address: address,
			}, nil
		}
	}

	peers, err := s.accountManager.GetStore().GetAccountPeers(ctx, store.LockingStrengthNone, accountID, "", "")
	if err != nil {
		return nil, fmt.Errorf("resolve peers for account: %w", err)
	}

	addr, ok := netip.AddrFromSlice(ip)
	for _, peer := range peers {
		peerIP, peerOK := netip.AddrFromSlice(peer.IP)
		if ok && peerOK && peerIP == addr {
			return fromPeer(peer, address), nil
		}
	}

	return &resolvedEndpoint{
		Type:    networktraffic.EndpointTypeUnknown,
		Name:    ip.String(),
		Address: address,
	}, nil
}

func fromPeer(peer *nbpeer.Peer, address string) *resolvedEndpoint {
	return &resolvedEndpoint{
		ID:          peer.ID,
		Type:        networktraffic.EndpointTypePeer,
		Name:        peer.Name,
		Address:     address,
		DNSLabel:    peer.DNSLabel,
		OS:          peer.Meta.OS,
		CountryCode: peer.Location.CountryCode,
		CityName:    peer.Location.CityName,
	}
}

func isPermanentFlowError(err error) bool {
	return strings.Contains(err.Error(), "parse public key") || strings.Contains(err.Error(), "flow fields are empty")
}
