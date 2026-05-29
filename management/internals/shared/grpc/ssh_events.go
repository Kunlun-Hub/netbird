package grpc

import (
	"context"
	"net"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/netbirdio/netbird/management/server/activity"
	nbContext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/peer"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/shared/management/proto"
)

func (s *Server) ReportSSHSessionEvent(ctx context.Context, req *proto.EncryptedMessage) (*proto.Empty, error) {
	sshEvent := &proto.SSHSessionEvent{}
	peerKey, err := s.parseRequest(ctx, req, sshEvent)
	if err != nil {
		return nil, err
	}

	targetPeer, err := s.accountManager.GetStore().GetPeerByPeerPubKey(ctx, store.LockingStrengthNone, peerKey.String())
	if err != nil {
		return nil, mapError(ctx, err)
	}

	activityID, ok := sshActivityFromCode(sshEvent.GetActivityCode())
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported SSH activity code %q", sshEvent.GetActivityCode())
	}

	sourcePeer := s.lookupSSHSourcePeer(ctx, targetPeer.AccountID, sshEvent.GetSourcePeerIp())
	initiatorID := sshEvent.GetActorUserId()
	if initiatorID == "unknown" {
		initiatorID = ""
	}
	if initiatorID == "" && sourcePeer != nil {
		initiatorID = sourcePeer.UserID
	}
	if initiatorID == "" {
		initiatorID = activity.SystemInitiator
	}

	meta := sshEventMeta(sshEvent, targetPeer, sourcePeer)

	eventCtx := context.WithoutCancel(ctx)
	eventCtx = context.WithValue(eventCtx, nbContext.AccountIDKey, targetPeer.AccountID) //nolint:staticcheck
	eventCtx = context.WithValue(eventCtx, nbContext.PeerIDKey, targetPeer.ID)           //nolint:staticcheck
	s.accountManager.StoreEvent(eventCtx, initiatorID, targetPeer.ID, targetPeer.AccountID, activityID, meta)

	return &proto.Empty{}, nil
}

func sshActivityFromCode(code string) (activity.Activity, bool) {
	switch code {
	case activity.PeerSSHSessionStarted.StringCode():
		return activity.PeerSSHSessionStarted, true
	case activity.PeerSSHSessionEnded.StringCode():
		return activity.PeerSSHSessionEnded, true
	case activity.PeerSSHSessionDenied.StringCode():
		return activity.PeerSSHSessionDenied, true
	case activity.PeerSSHAuthFailed.StringCode():
		return activity.PeerSSHAuthFailed, true
	case activity.PeerSSHPolicyDenied.StringCode():
		return activity.PeerSSHPolicyDenied, true
	default:
		return 0, false
	}
}

func (s *Server) lookupSSHSourcePeer(ctx context.Context, accountID, sourceIP string) *peer.Peer {
	if sourceIP == "" {
		return nil
	}
	ip := net.ParseIP(sourceIP)
	if ip == nil {
		return nil
	}
	sourcePeer, err := s.accountManager.GetStore().GetPeerByIP(ctx, store.LockingStrengthNone, accountID, ip)
	if err != nil {
		return nil
	}
	return sourcePeer
}

func sshEventMeta(sshEvent *proto.SSHSessionEvent, targetPeer, sourcePeer *peer.Peer) map[string]any {
	meta := map[string]any{
		"session_id":             sshEvent.GetSessionId(),
		"account_id":             targetPeer.AccountID,
		"actor_user_id":          sshEvent.GetActorUserId(),
		"actor_user_email":       sshEvent.GetActorUserEmail(),
		"source_peer_ip":         sshEvent.GetSourcePeerIp(),
		"client_ip":              sshEvent.GetSourcePeerIp(),
		"destination_peer_id":    targetPeer.ID,
		"destination_peer_name":  targetPeer.Name,
		"target_peer_id":         targetPeer.ID,
		"target_peer_name":       targetPeer.Name,
		"destination_local_user": sshEvent.GetDestinationLocalUser(),
		"access_method":          sshEvent.GetAccessMethod(),
		"session_type":           sshEvent.GetSessionType(),
		"result":                 sshEvent.GetResult(),
		"reason":                 sshEvent.GetReason(),
		"client_version":         sshEvent.GetClientVersion(),
	}

	if sourcePeer != nil {
		meta["source_peer_id"] = sourcePeer.ID
		meta["source_peer_name"] = sourcePeer.Name
		meta["source_peer_user_id"] = sourcePeer.UserID
	}

	if startedAt := sshEvent.GetStartedAt(); startedAt != nil {
		meta["started_at"] = startedAt.AsTime().UTC().Format(time.RFC3339Nano)
	}
	if endedAt := sshEvent.GetEndedAt(); endedAt != nil {
		meta["ended_at"] = endedAt.AsTime().UTC().Format(time.RFC3339Nano)
	}
	if duration := sshEvent.GetDuration(); duration != nil {
		meta["duration"] = duration.AsDuration().String()
		meta["duration_ms"] = duration.AsDuration().Milliseconds()
	}

	return meta
}
