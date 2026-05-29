package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/netbirdio/netbird/management/server/activity"
	"github.com/netbirdio/netbird/management/server/peer"
	"github.com/netbirdio/netbird/shared/management/proto"
)

func TestSSHActivityFromCode(t *testing.T) {
	tests := []struct {
		code string
		want activity.Activity
	}{
		{activity.PeerSSHSessionStarted.StringCode(), activity.PeerSSHSessionStarted},
		{activity.PeerSSHSessionEnded.StringCode(), activity.PeerSSHSessionEnded},
		{activity.PeerSSHSessionDenied.StringCode(), activity.PeerSSHSessionDenied},
		{activity.PeerSSHAuthFailed.StringCode(), activity.PeerSSHAuthFailed},
		{activity.PeerSSHPolicyDenied.StringCode(), activity.PeerSSHPolicyDenied},
	}

	for _, tt := range tests {
		got, ok := sshActivityFromCode(tt.code)
		require.True(t, ok)
		require.Equal(t, tt.want, got)
	}

	_, ok := sshActivityFromCode("peer.ssh.unknown")
	require.False(t, ok)
}

func TestSSHEventMeta(t *testing.T) {
	startedAt := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(3 * time.Second)
	event := &proto.SSHSessionEvent{
		SessionId:            "session-1",
		ActorUserId:          "user-1",
		ActorUserEmail:       "user@example.com",
		SourcePeerIp:         "100.64.0.10",
		DestinationLocalUser: "ubuntu",
		AccessMethod:         "web-ssh",
		SessionType:          "shell",
		Result:               "allowed",
		Reason:               "ok",
		ClientVersion:        "SSH-2.0-test",
		StartedAt:            timestamppb.New(startedAt),
		EndedAt:              timestamppb.New(endedAt),
		Duration:             durationpb.New(3 * time.Second),
	}
	targetPeer := &peer.Peer{ID: "target-peer", AccountID: "account-1", Name: "target"}
	sourcePeer := &peer.Peer{ID: "source-peer", Name: "source", UserID: "source-user"}

	meta := sshEventMeta(event, targetPeer, sourcePeer)

	require.Equal(t, "session-1", meta["session_id"])
	require.Equal(t, "account-1", meta["account_id"])
	require.Equal(t, "target-peer", meta["destination_peer_id"])
	require.Equal(t, "target-peer", meta["target_peer_id"])
	require.Equal(t, "target", meta["destination_peer_name"])
	require.Equal(t, "source-peer", meta["source_peer_id"])
	require.Equal(t, "source", meta["source_peer_name"])
	require.Equal(t, "ubuntu", meta["destination_local_user"])
	require.Equal(t, int64(3000), meta["duration_ms"])
	require.Equal(t, startedAt.Format(time.RFC3339Nano), meta["started_at"])
	require.Equal(t, endedAt.Format(time.RFC3339Nano), meta["ended_at"])
}
