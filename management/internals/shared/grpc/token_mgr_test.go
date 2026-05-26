package grpc

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"hash"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/management/internals/controllers/network_map"
	"github.com/netbirdio/netbird/management/internals/controllers/network_map/update_channel"
	"github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/groups"
	relayhandler "github.com/netbirdio/netbird/management/server/http/handlers/relays"
	"github.com/netbirdio/netbird/management/server/settings"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/proto"
	"github.com/netbirdio/netbird/util"
)

var TurnTestHost = &config.Host{
	Proto:    config.UDP,
	URI:      "turn:turn.netbird.io:77777",
	Username: "username",
	Password: "",
}

func TestTimeBasedAuthSecretsManager_GenerateCredentials(t *testing.T) {
	ttl := util.Duration{Duration: time.Hour}
	secret := "some_secret"
	peersManager := update_channel.NewPeersUpdateManager(nil)

	rc := &config.Relay{
		Addresses:      []string{"localhost:0"},
		CredentialsTTL: ttl,
		Secret:         secret,
	}

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	settingsMockManager := settings.NewMockManager(ctrl)
	groupsManager := groups.NewManagerMock()

	tested, err := NewTimeBasedAuthSecretsManager(peersManager, &config.TURNConfig{
		CredentialsTTL:       ttl,
		Secret:               secret,
		Turns:                []*config.Host{TurnTestHost},
		TimeBasedCredentials: true,
	}, rc, settingsMockManager, groupsManager)
	require.NoError(t, err)

	turnCredentials, err := tested.GenerateTurnToken()
	require.NoError(t, err)

	if turnCredentials.Payload == "" {
		t.Errorf("expected generated TURN username not to be empty, got empty")
	}
	if turnCredentials.Signature == "" {
		t.Errorf("expected generated TURN password not to be empty, got empty")
	}

	validateMAC(t, sha1.New, turnCredentials.Payload, turnCredentials.Signature, []byte(secret))

	relayCredentials, err := tested.GenerateRelayToken()
	require.NoError(t, err)

	if relayCredentials.Payload == "" {
		t.Errorf("expected generated relay payload not to be empty, got empty")
	}
	if relayCredentials.Signature == "" {
		t.Errorf("expected generated relay signature not to be empty, got empty")
	}

	hashedSecret := sha256.Sum256([]byte(secret))
	validateMAC(t, sha256.New, relayCredentials.Payload, relayCredentials.Signature, hashedSecret[:])
}

func TestTimeBasedAuthSecretsManager_PushRelayList(t *testing.T) {
	ttl := util.Duration{Duration: time.Hour}
	secret := "some_secret"
	peersManager := update_channel.NewPeersUpdateManager(nil)
	accountID := "account-id"
	peerA := "peer-a"
	peerB := "peer-b"
	peerC := "peer-c"
	channelA := peersManager.CreateChannel(context.Background(), peerA)
	channelB := peersManager.CreateChannel(context.Background(), peerB)

	rc := &config.Relay{
		Servers: []*config.RelayServer{
			{ID: "relay-a", Address: "rels://relay-a.example.com:443", Priority: 30},
			{ID: "relay-b", Address: "rels://relay-b.example.com:443", Priority: 40},
		},
		CredentialsTTL: ttl,
		Secret:         secret,
	}

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	settingsMockManager := settings.NewMockManager(ctrl)
	settingsMockManager.EXPECT().GetExtraSettings(gomock.Any(), accountID).Return(&types.ExtraSettings{}, nil).AnyTimes()
	groupsManager := groups.NewManagerMock()

	tested, err := NewTimeBasedAuthSecretsManager(peersManager, &config.TURNConfig{
		CredentialsTTL:       ttl,
		Secret:               secret,
		Turns:                []*config.Host{TurnTestHost},
		TimeBasedCredentials: true,
	}, rc, settingsMockManager, groupsManager)
	require.NoError(t, err)

	count := tested.PushRelayList(context.Background(), accountID, []string{peerB, peerC, peerA})
	require.Equal(t, 2, count)

	readUpdate := func(ch <-chan *network_map.UpdateMessage) *network_map.UpdateMessage {
		t.Helper()
		select {
		case update := <-ch:
			return update
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for relay list update")
			return nil
		}
	}

	updateA := readUpdate(channelA)
	require.NotNil(t, updateA)
	require.Equal(t, network_map.MessageTypeControlConfig, updateA.MessageType)
	relayA := updateA.Update.GetNetbirdConfig().GetRelay()
	require.NotNil(t, relayA)
	require.Equal(t, []string{"rels://relay-b.example.com:443", "rels://relay-a.example.com:443"}, relayA.GetUrls())
	require.NotEmpty(t, relayA.GetTokenPayload())
	require.NotEmpty(t, relayA.GetTokenSignature())

	updateB := readUpdate(channelB)
	require.NotNil(t, updateB)
	require.Equal(t, network_map.MessageTypeControlConfig, updateB.MessageType)
	relayB := updateB.Update.GetNetbirdConfig().GetRelay()
	require.NotNil(t, relayB)
	require.Equal(t, relayhandler.ActiveRelayAddresses(rc), relayB.GetUrls())
	require.NotEmpty(t, relayB.GetTokenPayload())
	require.NotEmpty(t, relayB.GetTokenSignature())
}

func TestTimeBasedAuthSecretsManager_PushRelayTokens(t *testing.T) {
	ttl := util.Duration{Duration: time.Hour}
	secret := "some_secret"
	peersManager := update_channel.NewPeersUpdateManager(nil)
	accountID := "account-id"
	peerA := "peer-a"
	peerB := "peer-b"
	peerC := "peer-c"
	channelA := peersManager.CreateChannel(context.Background(), peerA)
	channelB := peersManager.CreateChannel(context.Background(), peerB)

	rc := &config.Relay{
		Addresses:      []string{"localhost:0"},
		CredentialsTTL: ttl,
		Secret:         secret,
	}

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	settingsMockManager := settings.NewMockManager(ctrl)
	settingsMockManager.EXPECT().GetExtraSettings(gomock.Any(), accountID).Return(&types.ExtraSettings{}, nil).AnyTimes()
	groupsManager := groups.NewManagerMock()

	tested, err := NewTimeBasedAuthSecretsManager(peersManager, nil, rc, settingsMockManager, groupsManager)
	require.NoError(t, err)

	count := tested.PushRelayTokens(context.Background(), accountID, []string{peerB, peerC, peerA})
	require.Equal(t, 2, count)

	readUpdate := func(ch <-chan *network_map.UpdateMessage) *network_map.UpdateMessage {
		t.Helper()
		select {
		case update := <-ch:
			return update
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for relay token update")
			return nil
		}
	}

	expectedURLs := relayhandler.ActiveRelayAddresses(rc)
	for _, ch := range []<-chan *network_map.UpdateMessage{channelA, channelB} {
		update := readUpdate(ch)
		require.NotNil(t, update)
		require.Equal(t, network_map.MessageTypeControlConfig, update.MessageType)
		config := update.Update.GetNetbirdConfig()
		require.Empty(t, config.GetStuns())
		require.Empty(t, config.GetTurns())
		relay := config.GetRelay()
		require.NotNil(t, relay)
		require.Equal(t, expectedURLs, relay.GetUrls())
		require.NotEmpty(t, relay.GetTokenPayload())
		require.NotEmpty(t, relay.GetTokenSignature())
	}
}

func TestTimeBasedAuthSecretsManager_SetupRefresh(t *testing.T) {
	ttl := util.Duration{Duration: 2 * time.Second}
	secret := "some_secret"
	peersManager := update_channel.NewPeersUpdateManager(nil)
	peer := "some_peer"
	updateChannel := peersManager.CreateChannel(context.Background(), peer)

	rc := &config.Relay{
		Addresses:      []string{"localhost:0"},
		CredentialsTTL: ttl,
		Secret:         secret,
	}

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	settingsMockManager := settings.NewMockManager(ctrl)
	settingsMockManager.EXPECT().GetExtraSettings(gomock.Any(), "someAccountID").Return(&types.ExtraSettings{}, nil).AnyTimes()
	groupsManager := groups.NewManagerMock()

	tested, err := NewTimeBasedAuthSecretsManager(peersManager, &config.TURNConfig{
		CredentialsTTL:       ttl,
		Secret:               secret,
		Turns:                []*config.Host{TurnTestHost},
		TimeBasedCredentials: true,
	}, rc, settingsMockManager, groupsManager)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tested.SetupRefresh(ctx, "someAccountID", peer)

	if _, ok := tested.turnCancelMap[peer]; !ok {
		t.Errorf("expecting peer to be present in the turn cancel map, got not present")
	}

	if _, ok := tested.relayCancelMap[peer]; !ok {
		t.Errorf("expecting peer to be present in the relay cancel map, got not present")
	}

	var updates []*network_map.UpdateMessage

loop:
	for timeout := time.After(5 * time.Second); ; {
		select {
		case update := <-updateChannel:
			updates = append(updates, update)
		case <-timeout:
			break loop
		}

		if len(updates) >= 2 {
			break loop
		}
	}

	if len(updates) < 2 {
		t.Errorf("expecting at least 2 peer credentials updates, got %v", len(updates))
	}

	var turnUpdates, relayUpdates int
	var firstTurnUpdate, secondTurnUpdate *proto.ProtectedHostConfig
	var firstRelayUpdate, secondRelayUpdate *proto.RelayConfig

	for _, update := range updates {
		if turns := update.Update.GetNetbirdConfig().GetTurns(); len(turns) > 0 {
			turnUpdates++
			if turnUpdates == 1 {
				firstTurnUpdate = turns[0]
			} else {
				secondTurnUpdate = turns[0]
			}
		}
		if relay := update.Update.GetNetbirdConfig().GetRelay(); relay != nil {
			// avoid updating on turn updates since they also send relay credentials
			if update.Update.GetNetbirdConfig().GetTurns() == nil {
				relayUpdates++
				if relayUpdates == 1 {
					firstRelayUpdate = relay
				} else {
					secondRelayUpdate = relay
				}
			}
		}
	}

	if turnUpdates < 1 {
		t.Errorf("expecting at least 1 TURN credential update, got %v", turnUpdates)
	}
	if relayUpdates < 1 {
		t.Errorf("expecting at least 1 relay credential update, got %v", relayUpdates)
	}

	if firstTurnUpdate != nil && secondTurnUpdate != nil {
		if firstTurnUpdate.Password == secondTurnUpdate.Password {
			t.Errorf("expecting first TURN credential update password %v to be different from second, got equal", firstTurnUpdate.Password)
		}
	}

	if firstRelayUpdate != nil && secondRelayUpdate != nil {
		if firstRelayUpdate.TokenSignature == secondRelayUpdate.TokenSignature {
			t.Errorf("expecting first relay credential update signature %v to be different from second, got equal", firstRelayUpdate.TokenSignature)
		}
	}
}

func TestTimeBasedAuthSecretsManager_CancelRefresh(t *testing.T) {
	ttl := util.Duration{Duration: time.Hour}
	secret := "some_secret"
	peersManager := update_channel.NewPeersUpdateManager(nil)
	peer := "some_peer"

	rc := &config.Relay{
		Addresses:      []string{"localhost:0"},
		CredentialsTTL: ttl,
		Secret:         secret,
	}

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	settingsMockManager := settings.NewMockManager(ctrl)
	groupsManager := groups.NewManagerMock()

	tested, err := NewTimeBasedAuthSecretsManager(peersManager, &config.TURNConfig{
		CredentialsTTL:       ttl,
		Secret:               secret,
		Turns:                []*config.Host{TurnTestHost},
		TimeBasedCredentials: true,
	}, rc, settingsMockManager, groupsManager)
	require.NoError(t, err)

	tested.SetupRefresh(context.Background(), "someAccountID", peer)
	if _, ok := tested.turnCancelMap[peer]; !ok {
		t.Errorf("expecting peer to be present in turn cancel map, got not present")
	}
	if _, ok := tested.relayCancelMap[peer]; !ok {
		t.Errorf("expecting peer to be present in relay cancel map, got not present")
	}

	tested.CancelRefresh(peer)
	if _, ok := tested.turnCancelMap[peer]; ok {
		t.Errorf("expecting peer to be not present in turn cancel map, got present")
	}
	if _, ok := tested.relayCancelMap[peer]; ok {
		t.Errorf("expecting peer to be not present in relay cancel map, got present")
	}
}

func validateMAC(t *testing.T, algo func() hash.Hash, username string, actualMAC string, key []byte) {
	t.Helper()
	mac := hmac.New(algo, key)

	_, err := mac.Write([]byte(username))
	if err != nil {
		t.Fatal(err)
	}

	expectedMAC := mac.Sum(nil)
	decodedMAC, err := base64.StdEncoding.DecodeString(actualMAC)
	if err != nil {
		t.Fatal(err)
	}
	equal := hmac.Equal(decodedMAC, expectedMAC)

	if !equal {
		t.Errorf("expected password MAC to be %s. got %s", expectedMAC, decodedMAC)
	}
}
