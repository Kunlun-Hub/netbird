package relays

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/mock_server"
	nbpeer "github.com/netbirdio/netbird/management/server/peer"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/auth"
)

type relayConfigPusherMock struct {
	called          bool
	count           int
	pushedAccountID string
	pushedPeerIDs   []string
	pushedPeerCount int
}

func (m *relayConfigPusherMock) PushRelayList(_ context.Context, accountID string, peerIDs []string) int {
	m.called = true
	m.pushedAccountID = accountID
	m.pushedPeerIDs = append([]string(nil), peerIDs...)
	return m.count
}

func TestApplyRelayConfigPushesGlobalRelayList(t *testing.T) {
	const (
		accountID = "account-id"
		userID    = "user-id"
	)

	configPusher := &relayConfigPusherMock{count: 3}
	handler := &Handler{
		accountManager: &mock_server.MockAccountManager{
			GetAccountByIDFunc: func(_ context.Context, requestedAccountID, requestedUserID string) (*types.Account, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)

				return &types.Account{Id: accountID}, nil
			},
			GetPeersFunc: func(_ context.Context, requestedAccountID, requestedUserID, nameFilter, ipFilter string) ([]*nbpeer.Peer, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				require.Empty(t, nameFilter)
				require.Empty(t, ipFilter)
				return []*nbpeer.Peer{
					{ID: "peer-a", AccountID: accountID},
					{ID: "embedded-peer", AccountID: accountID, ProxyMeta: nbpeer.ProxyMeta{Embedded: true}},
					{ID: "peer-b", AccountID: accountID},
				}, nil
			},
		},
		configPusher: configPusher,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/relays/apply", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{
		AccountId: accountID,
		UserId:    userID,
	})

	recorder := httptest.NewRecorder()
	handler.applyRelayConfig(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, configPusher.called)
	require.Equal(t, accountID, configPusher.pushedAccountID)
	require.Equal(t, []string{"peer-a", "peer-b"}, configPusher.pushedPeerIDs)

	var response applyRelayConfigResponse
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	require.Equal(t, applyRelayConfigResponse{
		Status:      "ok",
		TargetPeers: 3,
	}, response)
}

func TestUpdateRelayPriorityUpdatesConfigAndPushesRelayList(t *testing.T) {
	const (
		accountID = "account-id"
		userID    = "user-id"
	)

	configPusher := &relayConfigPusherMock{count: 2}
	config := &nbconfig.Relay{
		Servers: []*nbconfig.RelayServer{
			{ID: "relay-a", Address: "rels://relay-a.example.com:443", Priority: 30},
		},
	}
	handler := &Handler{
		config: config,
		accountManager: &mock_server.MockAccountManager{
			GetStoreFunc: func() store.Store {
				return nil
			},
			GetPeersFunc: func(_ context.Context, requestedAccountID, requestedUserID, nameFilter, ipFilter string) ([]*nbpeer.Peer, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				require.Empty(t, nameFilter)
				require.Empty(t, ipFilter)
				return []*nbpeer.Peer{
					{ID: "peer-a", AccountID: accountID},
					{ID: "embedded-peer", AccountID: accountID, ProxyMeta: nbpeer.ProxyMeta{Embedded: true}},
					{ID: "peer-b", AccountID: accountID},
				}, nil
			},
		},
		configPusher: configPusher,
	}

	body, err := json.Marshal(updateRelayRequest{Priority: 80})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, "/api/relays/relay-a", bytes.NewReader(body))
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{
		AccountId: accountID,
		UserId:    userID,
	})
	req = mux.SetURLVars(req, map[string]string{"id": "relay-a"})

	recorder := httptest.NewRecorder()
	handler.updateRelay(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 80, config.Servers[0].Priority)
	require.True(t, configPusher.called)
	require.Equal(t, []string{"peer-a", "peer-b"}, configPusher.pushedPeerIDs)

	var response applyRelayConfigResponse
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	require.Equal(t, applyRelayConfigResponse{
		Status:      "ok",
		TargetPeers: 2,
	}, response)
}

func TestUpdateRelayPriorityUsesGeneratedIDForAddressOnlyRelay(t *testing.T) {
	const (
		accountID    = "account-id"
		userID       = "user-id"
		relayAddress = "rels://auto.relay.01012388.xyz:12580"
	)

	config := &nbconfig.Relay{
		Servers: []*nbconfig.RelayServer{
			{Address: relayAddress, Priority: 30},
		},
	}
	handler := &Handler{
		config: config,
		accountManager: &mock_server.MockAccountManager{
			GetStoreFunc: func() store.Store {
				return nil
			},
			GetPeersFunc: func(_ context.Context, requestedAccountID, requestedUserID, nameFilter, ipFilter string) ([]*nbpeer.Peer, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				require.Empty(t, nameFilter)
				require.Empty(t, ipFilter)
				return []*nbpeer.Peer{{ID: "peer-a", AccountID: accountID}}, nil
			},
		},
		configPusher: &relayConfigPusherMock{count: 1},
	}

	relayID := relayKey("", relayAddress)
	require.NotEqual(t, relayAddress, relayID)
	require.NotEmpty(t, relayID)

	body, err := json.Marshal(updateRelayRequest{Priority: 55})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, "/api/relays/"+relayID, bytes.NewReader(body))
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{
		AccountId: accountID,
		UserId:    userID,
	})
	req = mux.SetURLVars(req, map[string]string{"id": relayID})

	recorder := httptest.NewRecorder()
	handler.updateRelay(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 55, config.Servers[0].Priority)
}

func TestRelayAddressesForAccountSortsByGlobalPriority(t *testing.T) {
	config := &nbconfig.Relay{
		Servers: []*nbconfig.RelayServer{
			{ID: "relay-a", Address: "rels://relay-a.example.com:443", Priority: 40},
			{ID: "relay-b", Address: "rels://relay-b.example.com:443", Priority: 30},
			{ID: "relay-c", Address: "rels://relay-c.example.com:443", Priority: 50},
		},
	}
	addresses := RelayAddressesForAccount(config, nil)

	require.Equal(t, []string{
		"rels://relay-c.example.com:443",
		"rels://relay-a.example.com:443",
		"rels://relay-b.example.com:443",
	}, addresses)
}

func TestRelayServersForAccountIncludesPriorityWithoutPreferredFlag(t *testing.T) {
	config := &nbconfig.Relay{
		Servers: []*nbconfig.RelayServer{
			{ID: "relay-a", Address: "rels://relay-a.example.com:443", Priority: 40},
			{ID: "relay-b", Address: "rels://relay-b.example.com:443", Priority: 20},
			{ID: "relay-c", Address: "rels://relay-c.example.com:443", Priority: 60},
		},
	}
	relays := RelayServersForAccount(config, nil)

	require.Len(t, relays, 3)
	require.Equal(t, "relay-c", relays[0].ID)
	require.Equal(t, 60, relays[0].Priority)
	require.Equal(t, "relay-a", relays[1].ID)
	require.Equal(t, 40, relays[1].Priority)
	require.Equal(t, "relay-b", relays[2].ID)
	require.Equal(t, 20, relays[2].Priority)
}

func TestRelayServersForAccountKeepsHighestPriorityForDuplicateAddress(t *testing.T) {
	const relayAddress = "rels://auto.relay.01012388.xyz:12580"

	config := &nbconfig.Relay{
		Servers: []*nbconfig.RelayServer{
			{Address: relayAddress, Priority: 30},
		},
	}
	settings := &types.Settings{
		Extra: &types.ExtraSettings{
			RegisteredRelays: map[string]types.RegisteredRelay{
				"relay-auto": {
					ID:       "relay-auto",
					Address:  relayAddress,
					Priority: 40,
					LastSeen: time.Now(),
				},
			},
		},
	}

	relays := RelayServersForAccount(config, settings)

	require.Len(t, relays, 1)
	require.Equal(t, "relay-auto", relays[0].ID)
	require.Equal(t, relayAddress, relays[0].Address)
	require.Equal(t, 40, relays[0].Priority)
}

func TestVerifyRelaySetupTokenAcceptsExpiredLegacyToken(t *testing.T) {
	const (
		secret    = "relay-secret"
		accountID = "account-id"
	)

	token, err := signRelaySetupToken(secret, time.Now().Add(-time.Hour).Unix(), accountID)
	require.NoError(t, err)

	actualAccountID, err := verifyRelaySetupToken(token, secret)
	require.NoError(t, err)
	require.Equal(t, accountID, actualAccountID)
}
