package idp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/gorilla/mux"
	"github.com/netbirdio/netbird/idp/dex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/management/server/mock_server"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestConnectorGuardHandler_RedirectsStaleLocalLoginWhenLocalIsDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	storeMock := store.NewMockStore(ctrl)
	storeMock.EXPECT().GetAllAccounts(gomock.Any()).Return([]*types.Account{
		{
			Id:                     "account-1",
			IsDomainPrimaryAccount: true,
			CreatedAt:              time.Unix(1, 0),
		},
	}).AnyTimes()
	storeMock.EXPECT().GetAccountSettings(gomock.Any(), store.LockingStrengthNone, "account-1").Return(&types.Settings{
		EnabledLoginOptions: []types.LoginOption{
			types.CreateProviderLoginOption("zsso"),
			types.CreateProviderLoginOption("wechatwork"),
		},
	}, nil).AnyTimes()

	handler := newTestConnectorGuardHandler(storeMock, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/oauth2/auth/local/login?back=%2Foauth2%2Fauth%3Fclient_id%3Dnetbird-dashboard%26state%3Dabc", nil)
	req = mux.SetURLVars(req, map[string]string{"connector": "local"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusFound, recorder.Code)
	location := recorder.Header().Get("Location")
	assert.Contains(t, location, "/oauth2/auth")
	assert.Contains(t, location, "client_id=netbird-dashboard")
	assert.Contains(t, location, "prompt=select_account")
}

func TestConnectorGuardHandler_UsesDexClientAllowlistForStaleLocalLogin(t *testing.T) {
	handler := &connectorGuardHandler{
		embeddedIDP: testConnectorAllowChecker{
			allowAuthRequestFunc: func(_ context.Context, authRequestID, connectorID string) (bool, error) {
				assert.Equal(t, "old-auth-request", authRequestID)
				assert.Equal(t, "local", connectorID)
				return false, nil
			},
		},
		dexHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "/oauth2/auth/local/login?back=%2Foauth2%2Fauth%3Fclient_id%3Dnetbird-dashboard%26state%3Dabc&state=old-auth-request", nil)
	req = mux.SetURLVars(req, map[string]string{"connector": "local"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusFound, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Location"), "prompt=select_account")
}

func TestConnectorGuardHandler_AllowsEnabledConnector(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	storeMock := store.NewMockStore(ctrl)
	storeMock.EXPECT().GetAllAccounts(gomock.Any()).Return([]*types.Account{
		{
			Id:                     "account-1",
			IsDomainPrimaryAccount: true,
			CreatedAt:              time.Unix(1, 0),
		},
	}).AnyTimes()
	storeMock.EXPECT().GetAccountSettings(gomock.Any(), store.LockingStrengthNone, "account-1").Return(&types.Settings{
		EnabledLoginOptions: []types.LoginOption{
			types.CreateProviderLoginOption("zsso"),
		},
	}, nil).AnyTimes()

	handler := newTestConnectorGuardHandler(storeMock, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/oauth2/auth/zsso/login?back=%2Foauth2%2Fauth%3Fclient_id%3Dnetbird-dashboard", nil)
	req = mux.SetURLVars(req, map[string]string{"connector": "zsso"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func newTestConnectorGuardHandler(storeMock store.Store, dexHandler http.Handler) http.Handler {
	return &connectorGuardHandler{
		accountManager: &mock_server.MockAccountManager{
			GetStoreFunc: func() store.Store {
				return storeMock
			},
		},
		dexHandler: dexHandler,
	}
}

type testConnectorAllowChecker struct {
	allowAuthRequestFunc func(ctx context.Context, authRequestID, connectorID string) (bool, error)
	allowClientFunc      func(ctx context.Context, clientID, connectorID string) (bool, error)
}

func (t testConnectorAllowChecker) IsConnectorAllowedForAuthRequest(ctx context.Context, authRequestID, connectorID string) (bool, error) {
	if t.allowAuthRequestFunc != nil {
		return t.allowAuthRequestFunc(ctx, authRequestID, connectorID)
	}
	return true, nil
}

func (t testConnectorAllowChecker) IsConnectorAllowedForClient(ctx context.Context, clientID, connectorID string) (bool, error) {
	if t.allowClientFunc != nil {
		return t.allowClientFunc(ctx, clientID, connectorID)
	}
	return true, nil
}

func (t testConnectorAllowChecker) ListConnectors(context.Context) ([]*dex.ConnectorConfig, error) {
	return nil, nil
}
