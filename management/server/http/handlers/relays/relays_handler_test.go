package relays

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/mock_server"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/auth"
)

type relayConfigPusherMock struct {
	called bool
	count  int
}

func (m *relayConfigPusherMock) PushRelayList(_ context.Context) int {
	m.called = true
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

	var response applyRelayConfigResponse
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	require.Equal(t, applyRelayConfigResponse{
		Status:      "ok",
		TargetPeers: 3,
	}, response)
}
