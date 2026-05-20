package idp

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/netbirdio/netbird/idp/dex"
	"github.com/netbirdio/netbird/management/server/account"
	idpmanager "github.com/netbirdio/netbird/management/server/idp"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

const localConnectorID = "local"

type connectorGuardHandler struct {
	accountManager account.Manager
	embeddedIDP    connectorAllowChecker
	dexHandler     http.Handler
}

type connectorAllowChecker interface {
	IsConnectorAllowedForAuthRequest(ctx context.Context, authRequestID, connectorID string) (bool, error)
	IsConnectorAllowedForClient(ctx context.Context, clientID, connectorID string) (bool, error)
	ListConnectors(ctx context.Context) ([]*dex.ConnectorConfig, error)
}

func NewConnectorGuardHandler(accountManager account.Manager, embeddedIDP *idpmanager.EmbeddedIdPManager) http.Handler {
	return &connectorGuardHandler{
		accountManager: accountManager,
		embeddedIDP:    embeddedIDP,
		dexHandler:     embeddedIDP.Handler(),
	}
}

func (h *connectorGuardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	connectorID := mux.Vars(r)["connector"]
	if connectorID == "" {
		h.dexHandler.ServeHTTP(w, r)
		return
	}

	allowed, err := h.isConnectorAllowed(r, connectorID)
	if err != nil {
		log.Warnf("ConnectorGuardHandler: failed to resolve login settings: %v, passing to dex", err)
		h.dexHandler.ServeHTTP(w, r)
		return
	}

	if allowed {
		h.dexHandler.ServeHTTP(w, r)
		return
	}

	log.Infof("ConnectorGuardHandler: connector %s is no longer allowed, restarting auth flow", connectorID)
	http.Redirect(w, r, currentAuthURL(r), http.StatusFound)
}

func (h *connectorGuardHandler) isConnectorAllowed(r *http.Request, connectorID string) (bool, error) {
	ctx := r.Context()
	if allowed, resolved, err := h.isConnectorAllowedByDexClient(ctx, connectorID, r); resolved || err != nil {
		return allowed, err
	}

	settings := h.getPrimaryAccountSettings(ctx)
	if settings == nil {
		return true, nil
	}

	settings = settings.Copy()
	if settings.EnabledLoginOptions == nil {
		return h.isConnectorAllowedByLegacyLoginMethod(ctx, settings, connectorID)
	}
	if len(settings.EnabledLoginOptions) == 0 {
		return true, nil
	}

	for _, option := range settings.EnabledLoginOptions {
		switch {
		case option == types.LoginOptionEmail:
			if connectorID == localConnectorID && !settings.LocalAuthDisabled {
				return true, nil
			}
		case option.IsProviderLoginOption():
			if connectorID == option.GetProviderIDFromLoginOption() {
				return true, nil
			}
		}
	}

	return false, nil
}

func (h *connectorGuardHandler) isConnectorAllowedByDexClient(ctx context.Context, connectorID string, r *http.Request) (bool, bool, error) {
	if h.embeddedIDP == nil || r == nil {
		return false, false, nil
	}

	authRequestID := r.URL.Query().Get("state")
	if authRequestID != "" {
		allowed, err := h.embeddedIDP.IsConnectorAllowedForAuthRequest(ctx, authRequestID, connectorID)
		if err == nil {
			return allowed, true, nil
		}
		log.Warnf("ConnectorGuardHandler: failed to check auth request connector allowlist: %v", err)
	}

	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		clientID = clientIDFromBackURL(r)
	}
	if clientID == "" {
		return false, false, nil
	}

	allowed, err := h.embeddedIDP.IsConnectorAllowedForClient(ctx, clientID, connectorID)
	if err != nil {
		return false, false, err
	}
	return allowed, true, nil
}

func (h *connectorGuardHandler) isConnectorAllowedByLegacyLoginMethod(ctx context.Context, settings *types.Settings, connectorID string) (bool, error) {
	switch settings.LoginMethod {
	case types.LoginMethodEmail:
		return connectorID == localConnectorID && !settings.LocalAuthDisabled, nil
	case types.LoginMethodWeChatWork:
		wechatWorkConnectorID, err := h.getWeChatWorkConnectorID(ctx)
		if err != nil {
			return false, err
		}
		return connectorID == wechatWorkConnectorID, nil
	default:
		return true, nil
	}
}

func (h *connectorGuardHandler) getPrimaryAccountSettings(ctx context.Context) *types.Settings {
	accounts := h.accountManager.GetStore().GetAllAccounts(ctx)
	if len(accounts) == 0 {
		return nil
	}

	primaryAccount := accounts[0]
	for _, account := range accounts {
		if account.IsDomainPrimaryAccount {
			primaryAccount = account
			break
		}
	}

	settings, err := h.accountManager.GetStore().GetAccountSettings(ctx, store.LockingStrengthNone, primaryAccount.Id)
	if err != nil {
		return primaryAccount.Settings
	}
	if settings == nil {
		return primaryAccount.Settings
	}

	return settings
}

func (h *connectorGuardHandler) getWeChatWorkConnectorID(ctx context.Context) (string, error) {
	if h.embeddedIDP == nil {
		return "", nil
	}

	connectors, err := h.embeddedIDP.ListConnectors(ctx)
	if err != nil {
		return "", err
	}

	for _, connector := range connectors {
		if connector != nil && connector.Type == "wechatwork" {
			return connector.ID, nil
		}
	}

	return "", nil
}

func currentAuthURL(r *http.Request) string {
	target := r.URL.Query().Get("back")
	if target == "" {
		return "/oauth2/auth"
	}

	redirectURL, err := url.Parse(target)
	if err != nil || redirectURL.IsAbs() || !strings.HasPrefix(redirectURL.Path, "/oauth2/auth") {
		return "/oauth2/auth"
	}

	query := redirectURL.Query()
	if query.Get("prompt") == "" {
		query.Set("prompt", "select_account")
	}
	redirectURL.RawQuery = query.Encode()

	return redirectURL.String()
}

func clientIDFromBackURL(r *http.Request) string {
	backURL := r.URL.Query().Get("back")
	if backURL == "" {
		return ""
	}

	redirectURL, err := url.Parse(backURL)
	if err != nil || redirectURL.IsAbs() || !strings.HasPrefix(redirectURL.Path, "/oauth2/auth") {
		return ""
	}

	return redirectURL.Query().Get("client_id")
}
