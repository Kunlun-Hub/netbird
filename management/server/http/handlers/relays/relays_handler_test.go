package relays

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/mock_server"
	nbpeer "github.com/netbirdio/netbird/management/server/peer"
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

func (m *relayConfigPusherMock) PushRelayTokens(_ context.Context, accountID string, peerIDs []string) int {
	m.pushedAccountID = accountID
	m.pushedPeerIDs = append([]string(nil), peerIDs...)
	return m.pushedPeerCount
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

func TestSaveRelayPreferencesPushesOnlyChangedPeers(t *testing.T) {
	const (
		accountID = "account-id"
		userID    = "user-id"
	)

	oldSettings := &types.Settings{
		Extra: &types.ExtraSettings{
			RelayGroupPreferences: map[string][]string{
				"group-a": {"relay-a"},
			},
		},
	}
	account := relayPreferenceTestAccount(accountID)
	configPusher := &relayConfigPusherMock{pushedPeerCount: 1}

	handler := &Handler{
		config: &nbconfig.Relay{
			Servers: []*nbconfig.RelayServer{
				{ID: "relay-a", Address: "rels://relay-a.example.com:443"},
				{ID: "relay-b", Address: "rels://relay-b.example.com:443"},
			},
		},
		accountManager: &mock_server.MockAccountManager{
			GetAccountSettingsFunc: func(_ context.Context, requestedAccountID, requestedUserID string) (*types.Settings, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				return oldSettings, nil
			},
			GetPeersFunc: func(_ context.Context, requestedAccountID, requestedUserID, nameFilter, ipFilter string) ([]*nbpeer.Peer, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				require.Empty(t, nameFilter)
				require.Empty(t, ipFilter)
				return account.GetPeers(), nil
			},
			GetAllGroupsFunc: func(_ context.Context, requestedAccountID, requestedUserID string) ([]*types.Group, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				return []*types.Group{account.Groups["group-a"], account.Groups["group-b"]}, nil
			},
			GetAccountFunc: func(_ context.Context, requestedAccountID string) (*types.Account, error) {
				require.Equal(t, accountID, requestedAccountID)
				return account, nil
			},
			UpdateAccountSettingsFunc: func(_ context.Context, requestedAccountID, requestedUserID string, newSettings *types.Settings) (*types.Settings, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				require.Equal(t, map[string][]string{"group-a": {"relay-b"}}, newSettings.Extra.RelayGroupPreferences)
				require.Empty(t, newSettings.Extra.RelayPeerPreferences)
				return newSettings, nil
			},
		},
		configPusher: configPusher,
	}

	payload := relayPreferencesResponse{
		GroupPreferences: map[string][]string{"group-a": {"relay-b"}},
		PeerPreferences:  map[string][]string{},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/relays/preferences", bytes.NewReader(body))
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{
		AccountId: accountID,
		UserId:    userID,
	})

	recorder := httptest.NewRecorder()
	handler.saveRelayPreferences(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, accountID, configPusher.pushedAccountID)
	require.Equal(t, []string{"peer-a"}, configPusher.pushedPeerIDs)

	var response relayPreferencesResponse
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	require.Equal(t, 1, response.AppliedPeers)
	require.Equal(t, payload.GroupPreferences, response.GroupPreferences)
	require.Empty(t, response.PeerPreferences)
}

func TestPreferredRelayAddressesKeepsAllRelaysWithPreferredFirst(t *testing.T) {
	config := &nbconfig.Relay{
		Servers: []*nbconfig.RelayServer{
			{ID: "relay-a", Address: "rels://relay-a.example.com:443"},
			{ID: "relay-b", Address: "rels://relay-b.example.com:443"},
			{ID: "relay-c", Address: "rels://relay-c.example.com:443"},
		},
	}
	settings := &types.Settings{
		Extra: &types.ExtraSettings{
			RelayPeerPreferences: map[string][]string{
				"peer-a": {"relay-b"},
			},
		},
	}

	addresses := PreferredRelayAddresses(config, "peer-a", nil, settings)

	require.Equal(t, []string{
		"rels://relay-b.example.com:443",
		"rels://relay-a.example.com:443",
		"rels://relay-c.example.com:443",
	}, addresses)
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

func TestSaveRelayPreferencesSkipsPushWhenEffectiveRelayOrderDoesNotChange(t *testing.T) {
	const (
		accountID = "account-id"
		userID    = "user-id"
	)

	oldSettings := &types.Settings{
		Extra: &types.ExtraSettings{
			RelayGroupPreferences: map[string][]string{
				"group-a": {"relay-a"},
				"group-b": {"relay-b"},
			},
		},
	}
	account := relayPreferenceTestAccount(accountID)
	configPusher := &relayConfigPusherMock{pushedPeerCount: 1}

	handler := &Handler{
		config: &nbconfig.Relay{
			Servers: []*nbconfig.RelayServer{
				{ID: "relay-a", Address: "rels://relay-a.example.com:443"},
				{ID: "relay-b", Address: "rels://relay-b.example.com:443"},
			},
		},
		accountManager: &mock_server.MockAccountManager{
			GetAccountSettingsFunc: func(_ context.Context, requestedAccountID, requestedUserID string) (*types.Settings, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				return oldSettings, nil
			},
			GetPeersFunc: func(_ context.Context, requestedAccountID, requestedUserID, nameFilter, ipFilter string) ([]*nbpeer.Peer, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				require.Empty(t, nameFilter)
				require.Empty(t, ipFilter)
				return account.GetPeers(), nil
			},
			GetAllGroupsFunc: func(_ context.Context, requestedAccountID, requestedUserID string) ([]*types.Group, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				return []*types.Group{account.Groups["group-a"], account.Groups["group-b"]}, nil
			},
			GetAccountFunc: func(_ context.Context, requestedAccountID string) (*types.Account, error) {
				require.Equal(t, accountID, requestedAccountID)
				return account, nil
			},
			UpdateAccountSettingsFunc: func(_ context.Context, requestedAccountID, requestedUserID string, newSettings *types.Settings) (*types.Settings, error) {
				require.Equal(t, accountID, requestedAccountID)
				require.Equal(t, userID, requestedUserID)
				return newSettings, nil
			},
		},
		configPusher: configPusher,
	}

	payload := relayPreferencesResponse{
		GroupPreferences: map[string][]string{
			"group-a": {"relay-a"},
			"group-b": {"relay-b"},
		},
		PeerPreferences: map[string][]string{},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/relays/preferences", bytes.NewReader(body))
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{
		AccountId: accountID,
		UserId:    userID,
	})

	recorder := httptest.NewRecorder()
	handler.saveRelayPreferences(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, configPusher.pushedAccountID)
	require.Empty(t, configPusher.pushedPeerIDs)

	var response relayPreferencesResponse
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	require.Equal(t, 0, response.AppliedPeers)
	require.Equal(t, payload.GroupPreferences, response.GroupPreferences)
	require.Empty(t, response.PeerPreferences)
}

func relayPreferenceTestAccount(accountID string) *types.Account {
	return &types.Account{
		Id: accountID,
		Peers: map[string]*nbpeer.Peer{
			"peer-a": {
				ID:        "peer-a",
				AccountID: accountID,
			},
			"peer-b": {
				ID:        "peer-b",
				AccountID: accountID,
			},
			"embedded-peer": {
				ID:        "embedded-peer",
				AccountID: accountID,
				ProxyMeta: nbpeer.ProxyMeta{Embedded: true},
			},
		},
		Groups: map[string]*types.Group{
			"group-a": {
				ID:        "group-a",
				AccountID: accountID,
				Peers:     []string{"peer-a", "embedded-peer"},
			},
			"group-b": {
				ID:        "group-b",
				AccountID: accountID,
				Peers:     []string{"peer-b"},
			},
		},
	}
}
