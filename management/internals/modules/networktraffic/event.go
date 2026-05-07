package networktraffic

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/netbirdio/netbird/shared/management/http/api"
)

const (
	EndpointTypeUnknown      = "UNKNOWN"
	EndpointTypePeer         = "PEER"
	EndpointTypeHostResource = "HOST_RESOURCE"

	ConnectionTypeP2P    = "P2P"
	ConnectionTypeRouted = "ROUTED"
)

type Event struct {
	ID             string    `gorm:"primaryKey"`
	AccountID      string    `gorm:"index"`
	FlowID         string    `gorm:"index"`
	Timestamp      time.Time `gorm:"index"`
	EventType      string    `gorm:"index"`
	Direction      string    `gorm:"index"`
	Protocol       int       `gorm:"index"`
	ConnectionType string    `gorm:"index"`
	ReporterID     string    `gorm:"index"`
	UserID         string    `gorm:"index"`

	SourceID          string `gorm:"index"`
	SourceType        string `gorm:"index"`
	SourceName        string
	SourceAddress     string `gorm:"index"`
	SourceDNSLabel    string
	SourceOS          string
	SourceCountryCode string
	SourceCityName    string

	DestinationID          string `gorm:"index"`
	DestinationType        string `gorm:"index"`
	DestinationName        string
	DestinationAddress     string `gorm:"index"`
	DestinationDNSLabel    string
	DestinationOS          string
	DestinationCountryCode string
	DestinationCityName    string

	PolicyID   string
	PolicyName string

	ICMPType int
	ICMPCode int

	RxBytes   int64
	RxPackets int64
	TxBytes   int64
	TxPackets int64

	DNSDomain    string
	DNSQueryType string
	DNSAnswers   []string `gorm:"serializer:json"`
	DNSRCode     string

	UserName  string
	UserEmail string
}

func FormatAddress(ip net.IP, port uint32) string {
	if ip == nil {
		return ""
	}
	if port == 0 {
		return ip.String()
	}
	return net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))
}

func (e *Event) ToAPIResponse() *api.NetworkTrafficEvent {
	return &api.NetworkTrafficEvent{
		FlowId:      e.FlowID,
		Direction:   e.Direction,
		Protocol:    e.Protocol,
		ReporterId:  e.ReporterID,
		RxBytes:     int(e.RxBytes),
		RxPackets:   int(e.RxPackets),
		TxBytes:     int(e.TxBytes),
		TxPackets:   int(e.TxPackets),
		Dns:         e.toDNSInfo(),
		Source:      e.toEndpoint(true),
		Destination: e.toEndpoint(false),
		Policy:      api.NetworkTrafficPolicy{Id: e.PolicyID, Name: e.PolicyName},
		Icmp:        api.NetworkTrafficICMP{Type: e.ICMPType, Code: e.ICMPCode},
		User:        api.NetworkTrafficUser{Id: e.UserID, Name: e.UserName, Email: e.UserEmail},
		Events: []api.NetworkTrafficSubEvent{{
			Timestamp: e.Timestamp,
			Type:      e.EventType,
		}},
	}
}

func (e *Event) toDNSInfo() *api.NetworkTrafficDNSInfo {
	if e.DNSDomain == "" && e.DNSQueryType == "" && len(e.DNSAnswers) == 0 && e.DNSRCode == "" {
		return nil
	}

	answers := make([]string, 0, len(e.DNSAnswers))
	for _, answer := range e.DNSAnswers {
		answer = strings.TrimSpace(answer)
		if answer != "" {
			answers = append(answers, answer)
		}
	}

	return &api.NetworkTrafficDNSInfo{
		Domain:    stringPtr(e.DNSDomain),
		Query:     stringPtr(e.DNSDomain),
		QueryName: stringPtr(e.DNSDomain),
		Type:      stringPtr(e.DNSQueryType),
		QueryType: stringPtr(e.DNSQueryType),
		Answers:   &answers,
		Result:    &answers,
		Rcode:     stringPtr(e.DNSRCode),
	}
}

func (e *Event) toEndpoint(source bool) api.NetworkTrafficEndpoint {
	endpoint := api.NetworkTrafficEndpoint{
		Id:          e.DestinationID,
		Type:        e.DestinationType,
		Name:        e.DestinationName,
		Address:     e.DestinationAddress,
		DnsLabel:    stringPtr(e.DestinationDNSLabel),
		Os:          stringPtr(e.DestinationOS),
		GeoLocation: api.NetworkTrafficLocation{CountryCode: e.DestinationCountryCode, CityName: e.DestinationCityName},
	}

	if source {
		endpoint.Id = e.SourceID
		endpoint.Type = e.SourceType
		endpoint.Name = e.SourceName
		endpoint.Address = e.SourceAddress
		endpoint.DnsLabel = stringPtr(e.SourceDNSLabel)
		endpoint.Os = stringPtr(e.SourceOS)
		endpoint.GeoLocation = api.NetworkTrafficLocation{
			CountryCode: e.SourceCountryCode,
			CityName:    e.SourceCityName,
		}
	}

	return endpoint
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
