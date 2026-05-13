package idp

import (
	"context"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/netbirdio/netbird/idp/dex"
	"github.com/netbirdio/netbird/management/server/account"
	idpmanager "github.com/netbirdio/netbird/management/server/idp"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

type loginPreferenceHandler struct {
	accountManager account.Manager
	embeddedIDP    *idpmanager.EmbeddedIdPManager
	dexHandler     http.Handler
}

func NewLoginPreferenceHandler(accountManager account.Manager, embeddedIDP *idpmanager.EmbeddedIdPManager) http.Handler {
	return &loginPreferenceHandler{
		accountManager: accountManager,
		embeddedIDP:    embeddedIDP,
		dexHandler:     embeddedIDP.Handler(),
	}
}

func (h *loginPreferenceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Infof("LoginPreferenceHandler: Received request to %s", r.URL.Path)

	enabledOptions, connectors, err := h.resolveLoginOptions(r.Context())
	if err != nil {
		log.Infof("LoginPreferenceHandler: Error resolving login options: %v, passing to dex", err)
		h.dexHandler.ServeHTTP(w, r)
		return
	}

	log.Infof("LoginPreferenceHandler: Enabled login options: %v", enabledOptions)

	// Calculate all available login options count
	var availableOptionsCount int
	// Check if local auth is enabled
	settings := h.getPrimaryAccountSettings(r.Context())
	if settings != nil && !settings.LocalAuthDisabled {
		availableOptionsCount++
	}
	// Add all connectors
	availableOptionsCount += len(connectors)

	log.Infof("LoginPreferenceHandler: Available login options count: %d", availableOptionsCount)

	// If no specific options are enabled (all enabled) OR user has enabled all available options, pass to dex
	if len(enabledOptions) == 0 || (len(enabledOptions) == availableOptionsCount && availableOptionsCount > 0) {
		log.Infof("LoginPreferenceHandler: All options enabled, passing to dex")
		h.dexHandler.ServeHTTP(w, r)
		return
	}

	// Single option enabled - check if we need to redirect
	if len(enabledOptions) == 1 {
		singleOption := enabledOptions[0]
		var preferredConnectorID string
		if singleOption == types.LoginOptionEmail {
			preferredConnectorID = "local"
		} else if singleOption.IsProviderLoginOption() {
			preferredConnectorID = singleOption.GetProviderIDFromLoginOption()
		}

		if preferredConnectorID == "" {
			log.Infof("LoginPreferenceHandler: No preferred connector, passing to dex")
			h.dexHandler.ServeHTTP(w, r)
			return
		}

		switch {
		case r.URL.Path == "/oauth2/auth":
			log.Infof("LoginPreferenceHandler: Redirecting main login screen to %s", preferredConnectorID)
			h.redirectToPreferredConnector(w, r, preferredConnectorID)
			return
		case strings.HasPrefix(r.URL.Path, "/oauth2/auth/"):
			// Extract requested connector ID
			requestedConnectorID := strings.TrimPrefix(r.URL.Path, "/oauth2/auth/")
			requestedConnectorID = strings.Split(requestedConnectorID, "?")[0]
			requestedConnectorID = strings.Split(requestedConnectorID, "/")[0]

			if requestedConnectorID != preferredConnectorID {
				log.Infof("LoginPreferenceHandler: Requested connector %s differs from preferred %s, redirecting", requestedConnectorID, preferredConnectorID)
				h.redirectToPreferredConnector(w, r, preferredConnectorID)
				return
			}
		case strings.HasPrefix(r.URL.Path, "/oauth2/auth/local/login") || 
			strings.HasPrefix(r.URL.Path, "/oauth2/auth/") && strings.Contains(r.URL.Path, "/login"):
			// Check if the requested path contains a different connector than preferred
			if !strings.Contains(r.URL.Path, "/"+preferredConnectorID+"/") {
				log.Infof("LoginPreferenceHandler: Requested login path differs from preferred %s, redirecting", preferredConnectorID)
				h.redirectToPreferredConnector(w, r, preferredConnectorID)
				return
			}
		}

		log.Infof("LoginPreferenceHandler: Passing request to dex")
		h.dexHandler.ServeHTTP(w, r)
		return
	}

	// Multiple options enabled - show custom login selector
	if r.URL.Path == "/oauth2/auth" {
		log.Infof("LoginPreferenceHandler: Multiple login options enabled, showing custom selector")
		h.showLoginSelector(w, r, enabledOptions, connectors)
		return
	}

	log.Infof("LoginPreferenceHandler: Passing request to dex")
	h.dexHandler.ServeHTTP(w, r)
}

func (h *loginPreferenceHandler) redirectToPreferredConnector(w http.ResponseWriter, r *http.Request, connectorID string) {
	if connectorID == "" {
		log.Infof("LoginPreferenceHandler: Connector ID is empty, passing to dex")
		h.dexHandler.ServeHTTP(w, r)
		return
	}

	redirectURL := *r.URL
	redirectURL.Path = "/oauth2/auth/" + connectorID
	log.Infof("LoginPreferenceHandler: Redirecting to %s", redirectURL.String())
	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

func (h *loginPreferenceHandler) resolveLoginOptions(ctx context.Context) ([]types.LoginOption, []*dex.ConnectorConfig, error) {
	settings := h.getPrimaryAccountSettings(ctx)
	if settings == nil {
		log.Infof("LoginPreferenceHandler: No settings found for primary account, enabling all options")
		return []types.LoginOption{}, nil, nil
	}

	// Use Copy to ensure we have default values
	settings = settings.Copy()

	// First, check if we have EnabledLoginOptions
	if len(settings.EnabledLoginOptions) > 0 {
		log.Infof("LoginPreferenceHandler: Found enabled login options: %v", settings.EnabledLoginOptions)
	} else {
		// Fall back to deprecated LoginMethod for backward compatibility
		log.Infof("LoginPreferenceHandler: No enabled options found, falling back to deprecated login method: %v", settings.LoginMethod)
		options, err := h.convertLegacyLoginMethodToOptions(ctx, settings.LoginMethod)
		if err != nil {
			return nil, nil, err
		}
		return options, nil, nil
	}

	// Get all available connectors to validate options
	connectors, err := h.embeddedIDP.ListConnectors(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Validate and filter enabled options
	validOptions := h.validateLoginOptions(settings.EnabledLoginOptions, connectors, settings.LocalAuthDisabled)

	return validOptions, connectors, nil
}

func (h *loginPreferenceHandler) validateLoginOptions(options []types.LoginOption, connectors []*dex.ConnectorConfig, localAuthDisabled bool) []types.LoginOption {
	validOptions := []types.LoginOption{}

	for _, option := range options {
		if option == types.LoginOptionEmail {
			if !localAuthDisabled {
				validOptions = append(validOptions, option)
			}
			continue
		}

		if option.IsProviderLoginOption() {
			providerID := option.GetProviderIDFromLoginOption()
			// Check if the connector exists
			for _, connector := range connectors {
				if connector.ID == providerID {
					validOptions = append(validOptions, option)
					break
				}
			}
		}
	}

	// If no valid options left, return empty (enable all)
	if len(validOptions) == 0 && len(options) > 0 {
		log.Infof("LoginPreferenceHandler: All enabled options invalid, enabling all")
		return []types.LoginOption{}
	}

	return validOptions
}

func (h *loginPreferenceHandler) convertLegacyLoginMethodToOptions(ctx context.Context, loginMethod string) ([]types.LoginOption, error) {
	switch loginMethod {
	case "email":
		return []types.LoginOption{types.LoginOptionEmail}, nil
	case "wechatwork":
		connectorID, err := h.getWeChatWorkConnectorID(ctx)
		if err != nil {
			return nil, err
		}
		if connectorID != "" {
			return []types.LoginOption{types.CreateProviderLoginOption(connectorID)}, nil
		}
		return []types.LoginOption{}, nil
	default:
		// "all" or any other value means all options enabled
		return []types.LoginOption{}, nil
	}
}

func (h *loginPreferenceHandler) getPrimaryAccountSettings(ctx context.Context) *types.Settings {
	accounts := h.accountManager.GetStore().GetAllAccounts(ctx)
	log.Infof("LoginPreferenceHandler: Found %d accounts", len(accounts))

	if len(accounts) == 0 {
		log.Infof("LoginPreferenceHandler: No accounts found")
		return nil
	}

	// Find primary account
	var primaryAccount *types.Account
	for _, account := range accounts {
		log.Infof("LoginPreferenceHandler: Checking account: id=%s, IsDomainPrimaryAccount=%v, domain=%s", account.Id, account.IsDomainPrimaryAccount, account.Domain)
		if account.IsDomainPrimaryAccount {
			primaryAccount = account
			break
		}
	}

	// If no primary account found, use first account
	if primaryAccount == nil {
		log.Infof("LoginPreferenceHandler: No primary account found, using first account")
		primaryAccount = accounts[0]
	}

	log.Infof("LoginPreferenceHandler: Selected primary account: id=%s, IsDomainPrimaryAccount=%v", primaryAccount.Id, primaryAccount.IsDomainPrimaryAccount)

	// Use GetAccountSettings to specifically get account settings
	settings, err := h.accountManager.GetStore().GetAccountSettings(ctx, store.LockingStrengthNone, primaryAccount.Id)
	if err != nil {
		log.Infof("LoginPreferenceHandler: Failed to get account settings from store: %v, falling back to account settings", err)
		if primaryAccount.Settings != nil {
			log.Infof("LoginPreferenceHandler: Primary account settings (fallback)")
		} else {
			log.Infof("LoginPreferenceHandler: Primary account has no settings (fallback)")
		}
		return primaryAccount.Settings
	}

	if settings != nil {
		log.Infof("LoginPreferenceHandler: Primary account settings from store")
	} else {
		log.Infof("LoginPreferenceHandler: Primary account has no settings from store")
	}

	return settings
}

func (h *loginPreferenceHandler) getWeChatWorkConnectorID(ctx context.Context) (string, error) {
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

func (h *loginPreferenceHandler) showLoginSelector(w http.ResponseWriter, r *http.Request, enabledOptions []types.LoginOption, connectors []*dex.ConnectorConfig) {
	// Build login options for template
	type LoginOption struct {
		ID    string
		Name  string
		Type  string
		Local bool
	}

	var options []LoginOption

	for _, opt := range enabledOptions {
		if opt == types.LoginOptionEmail {
			options = append(options, LoginOption{
				ID:    "local",
				Name:  "继续使用 Email",
				Type:  "local",
				Local: true,
			})
		} else if opt.IsProviderLoginOption() {
			providerID := opt.GetProviderIDFromLoginOption()
			for _, connector := range connectors {
				if connector != nil && connector.ID == providerID {
					name := connector.Name
					if name == "" {
						name = getProviderNameByType(connector.Type)
					}
					options = append(options, LoginOption{
						ID:    connector.ID,
						Name:  "继续使用 " + name,
						Type:  connector.Type,
						Local: false,
					})
					break
				}
			}
		}
	}

	// Build HTML page with Dex-like styling
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>云链-Cloink</title>
    <style>
        *,:after,:before{box-sizing:border-box;border:0 solid #e5e7eb}
        html,body{margin:0;padding:0;font-family:ui-sans-serif,system-ui,sans-serif,Apple Color Emoji,Segoe UI Emoji;font-size:14px;line-height:1.5;background-color:#18191d;color:#e4e7e9;min-height:100vh}
        .nb-container{max-width:820px;margin:0 auto;padding:40px 20px;display:flex;flex-direction:column;align-items:center;justify-content:center;min-height:100vh}
        .nb-logo{width:180px;margin-bottom:40px}
        .nb-card{background-color:#1b1f22;border:1px solid rgba(50,54,61,.5);border-radius:12px;padding:40px;width:100%;max-width:400px;box-shadow:0 20px 25px -5px rgba(0,0,0,.1),0 8px 10px -6px rgba(0,0,0,.1)}
        .nb-heading{font-size:24px;font-weight:500;text-align:center;margin:0 0 24px 0;color:#fff}
        .nb-subheading{font-size:14px;color:rgba(167,177,185,.8);text-align:center;margin-bottom:24px}
        .nb-btn-connector{width:100%;padding:12px 20px;background-color:rgba(63,68,75,.5);border:1px solid rgba(63,68,75,.8);border-radius:8px;color:#e4e7e9;font-size:14px;font-weight:500;cursor:pointer;transition:all .2s;display:flex;align-items:center;justify-content:flex-start;text-decoration:none;margin-bottom:12px;gap:12px}
        .nb-btn-connector:hover{background-color:rgba(63,68,75,.8);border-color:rgba(63,68,75,1)}
        .nb-btn-connector .nb-icon{width:20px;height:20px;flex-shrink:0;background-size:contain;background-position:center;background-repeat:no-repeat}
        .nb-icon-email{background-image:url("data:image/svg+xml;charset=utf-8,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24' viewBox='0 0 24 24' fill='none' stroke='%23a7b1b9' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Crect x='2' y='4' width='20' height='16' rx='2'/%3E%3Cpath d='m22 7-8.97 5.7a1.94 1.94 0 0 1-2.06 0L2 7'/%3E%3C/svg%3E")}
        .nb-icon-default{background-image:url("data:image/svg+xml;charset=utf-8,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24' viewBox='0 0 24 24' fill='none' stroke='%23a7b1b9' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4'/%3E%3Cpolyline points='10 17 15 12 10 7'/%3E%3Cline x1='15' y1='12' x2='3' y2='12'/%3E%3C/svg%3E")}
    </style>
</head>
<body>
    <div class="nb-container">
        <div class="nb-logo">
            <svg xmlns="http://www.w3.org/2000/svg" version="1.1" viewBox="0 0 266 46" style="shape-rendering:geometricPrecision; text-rendering:geometricPrecision; image-rendering:optimizeQuality; fill-rule:evenodd; clip-rule:evenodd" xmlns:xlink="http://www.w3.org/1999/xlink">
            <g><path style="opacity:1" fill="#08a8f9" d="M 44.5,45.5 C 33.8333,45.5 23.1667,45.5 12.5,45.5C 10.1495,43.6408 8.31617,41.3075 7,38.5C 6.33333,28.1667 6.33333,17.8333 7,7.5C 8.5,4.66667 10.6667,2.5 13.5,1C 23.8333,0.333333 34.1667,0.333333 44.5,1C 47,2.16667 48.8333,4 50,6.5C 50.6667,17.5 50.6667,28.5 50,39.5C 48.6403,42.0226 46.807,44.0226 44.5,45.5 Z"/></g>
            <g><path style="opacity:1" fill="#ecedec" d="M 109.5,8.5 C 111.312,8.77128 112.978,9.43795 114.5,10.5C 113.157,12.7841 113.824,13.6174 116.5,13C 115.167,12.3333 115.167,11.6667 116.5,11C 117.793,10.51 119.127,10.3433 120.5,10.5C 120.343,11.8734 120.51,13.2068 121,14.5C 122.356,13.6198 123.856,13.2865 125.5,13.5C 125.162,8.6687 127.162,7.33537 131.5,9.5C 131.67,10.8221 131.337,11.9887 130.5,13C 132.81,13.4966 135.143,13.6633 137.5,13.5C 137.5,14.8333 137.5,16.1667 137.5,17.5C 135.304,17.41 133.137,17.0767 131,16.5C 130.167,16.8333 129.333,17.1667 128.5,17.5C 129.689,18.4287 131.022,18.762 132.5,18.5C 132.67,19.8221 132.337,20.9887 131.5,22C 133.134,22.4935 134.801,22.6602 136.5,22.5C 136.5,23.8333 136.5,25.1667 136.5,26.5C 134.712,26.2148 133.045,26.5481 131.5,27.5C 133.396,28.4656 135.396,28.7989 137.5,28.5C 137.5,29.8333 137.5,31.1667 137.5,32.5C 135.5,32.5 133.5,32.5 131.5,32.5C 131.421,33.9305 131.754,35.2638 132.5,36.5C 134.5,37.1667 136.5,37.8333 138.5,38.5C 137.754,39.7362 137.421,41.0695 137.5,42.5C 131.343,43.0806 125.51,42.0806 120,39.5C 118.165,42.858 115.998,43.1913 113.5,40.5C 110.588,43.3103 108.255,42.8103 106.5,39C 108.314,36.5185 108.98,33.6852 108.5,30.5C 107.5,30.5 106.5,30.5 105.5,30.5C 105.5,29.1667 105.5,27.8333 105.5,26.5C 106.5,26.5 107.5,26.5 108.5,26.5C 107.134,23.9359 105.467,21.4359 103.5,19C 106.409,16.0151 108.409,12.5151 109.5,8.5 Z M 116.5,16.5 C 118.139,18.6393 120.139,18.9726 122.5,17.5C 123.561,20.2945 122.394,21.6279 119,21.5C 115.883,20.4607 112.716,19.6273 109.5,19C 111.833,17.957 114.166,17.1237 116.5,16.5 Z M 112.5,23.5 C 114.011,24.5025 115.678,25.1692 117.5,25.5C 117.665,28.8499 117.498,32.1832 117,35.5C 116.593,31.6695 115.26,31.0028 113,33.5C 112.517,32.552 112.351,31.552 112.5,30.5C 113.833,30.5 115.167,30.5 116.5,30.5C 116.5,29.1667 116.5,27.8333 116.5,26.5C 115.167,26.5 113.833,26.5 112.5,26.5C 112.5,25.5 112.5,24.5 112.5,23.5 Z M 121.5,26.5 C 123.604,26.2011 125.604,26.5344 127.5,27.5C 126.5,27.8333 125.5,28.1667 124.5,28.5C 122.5,28.5 120.5,28.5 118.5,28.5C 119.833,26.8333 120.667,26.1667 121,25.5C 119.5,25.1667 117.833,24.8333 116,24.5C 117.665,24.1667 119.165,23.8333 120.5,23.5C 121.5,24.5 121.5,25.5 121.5,26.5 Z M 121.5,20.5 C 123.167,21.1667 125.167,20.8333 127.5,19.5C 127.5,21.1667 127.5,22.8333 127.5,24.5C 127.5,25.5 127.5,26.5 127.5,27.5C 126.833,27.1667 126.167,26.8333 125.5,26.5C 126.167,25.1667 126.167,24.1667 125.5,22.5C 124.5,22.5 123.5,22.5 122.5,22.5C 122.167,21.5 121.833,21 121.5,20.5 Z M 112.5,16.5 C 113.667,17.1667 114.833,17.1667 116,16.5C 115.667,17.8333 115.833,18.6667 116.5,19.5C 115.333,19.5 114.167,19.5 113,19.5C 112.667,18.5 112.667,17.5 112.5,16.5 Z"/></g>
            <g><path style="opacity:1" fill="#ecedec" d="M 72.5,11.5 C 80.1705,12.7784 87.8372,12.7784 95.5,11.5C 95.5,13.1667 95.5,14.8333 95.5,16.5C 87.8333,16.5 80.1667,16.5 72.5,16.5C 72.5,14.8333 72.5,13.1667 72.5,11.5 Z"/></g>
            <g><path style="opacity:1" fill="#f1f6f9" d="M 37.5,33.5 C 31.9738,33.8214 26.6405,33.4881 21.5,32.5C 22.4778,32.189 23.1445,31.5223 23.5,30.5C 14.2865,25.9432 13.2865,20.1098 20.5,13C 26.1667,12.3333 31.8333,12.3333 37.5,13C 36.3128,14.0195 35.3128,15.1861 34.5,16.5C 41.8273,18.8194 43.994,23.4861 41,30.5C 39.9609,31.7101 38.7942,32.7101 37.5,33.5 Z"/></g>
            <g><path style="opacity:1" fill="#ecedec" d="M 160.5,15.5 C 164.514,15.3345 168.514,15.5012 172.5,16C 173.26,17.4411 173.926,18.9411 174.5,20.5C 170.952,20.6186 167.285,20.7853 163.5,21C 156.489,27.4704 157.489,32.3038 166.5,35.5C 168.782,34.7766 171.116,34.4433 173.5,34.5C 173.5,36.1667 173.5,37.8333 173.5,39.5C 166.52,41.4823 160.02,40.4823 154,36.5C 149.271,27.1923 151.438,20.1923 160.5,15.5 Z"/></g>
            <g><path style="opacity:1" fill="#ecedec" d="M 177.5,15.5 C 179.833,15.5 182.167,15.5 184.5,15.5C 184.621,23.8578 184.288,32.1912 183.5,40.5C 181.167,40.5 178.833,40.5 176.5,40.5C 177.407,32.2079 177.74,23.8746 177.5,15.5 Z"/></g>
            <g><path style="opacity:1" fill="#ecedec" d="M 211.5,15.5 C 213.833,15.5 216.167,15.5 218.5,15.5C 218.5,16.8333 218.5,18.1667 218.5,19.5C 216.167,19.5 213.833,19.5 211.5,19.5C 211.5,18.1667 211.5,16.8333 211.5,15.5 Z"/></g>
            <g><path style="opacity:1" fill="#ecedec" d="M 265.5,21.5 C 265.5,21.8333 265.5,22.1667 265.5,22.5C 263.037,24.4611 260.703,26.6278 258.5,29C 260.488,32.5013 262.822,35.668 265.5,38.5C 265.5,39.1667 265.5,39.8333 265.5,40.5C 262.753,40.8134 260.086,40.48 257.5,39.5C 253.375,30.0103 251.708,30.3437 252.5,40.5C 250.167,40.5 247.833,40.5 245.5,40.5C 246.371,32.2004 246.705,23.8671 246.5,15.5C 248.833,15.5 251.167,15.5 253.5,15.5C 253.335,18.8499 253.502,22.1832 254,25.5C 256.944,21.8864 260.777,20.5531 265.5,21.5 Z"/></g>
            <g><path style="opacity:1" fill="#49bcf7" d="M 23.5,19.5 C 26.6399,18.3592 29.9733,18.1925 33.5,19C 32.9558,19.7172 32.2891,20.2172 31.5,20.5C 29.0521,19.5269 26.3854,19.1936 23.5,19.5 Z"/></g>
            <g><path style="opacity:1" fill="#14a6f9" d="M 23.5,19.5 C 26.3854,19.1936 29.0521,19.5269 31.5,20.5C 31.1961,21.1499 30.8627,21.8165 30.5,22.5C 32.3413,23.2304 34.1746,23.8971 36,24.5C 36.7252,26.1575 36.2252,27.1575 34.5,27.5C 31.6146,27.8064 28.9479,27.4731 26.5,26.5C 26.8333,25.8333 27.1667,25.1667 27.5,24.5C 28.0431,24.44 28.3764,24.1067 28.5,23.5C 26.9547,22.5481 25.288,22.2148 23.5,22.5C 22.1667,21.5 22.1667,20.5 23.5,19.5 Z"/></g>
            <g><path style="opacity:1" fill="#ecedec" d="M 68.5,20.5 C 78.8358,21.7966 89.1691,21.7966 99.5,20.5C 99.5,22.1667 99.5,23.8333 99.5,25.5C 94.8215,25.3342 90.1548,25.5008 85.5,26C 85.9574,26.414 86.2907,26.914 86.5,27.5C 84.1765,30.1261 81.8432,32.7928 79.5,35.5C 82.8757,36.7105 86.209,36.5438 89.5,35C 85.6321,32.2428 85.7988,30.0761 90,28.5C 93.7132,31.3928 96.5466,35.0594 98.5,39.5C 96.0244,42.0691 93.5244,42.0691 91,39.5C 84.1955,40.3954 77.3622,41.062 70.5,41.5C 70.5792,40.0695 70.2458,38.7362 69.5,37.5C 74.2251,35.1078 77.5584,31.4412 79.5,26.5C 75.8927,25.5108 72.226,25.1774 68.5,25.5C 68.5,23.8333 68.5,22.1667 68.5,20.5 Z"/></g>
            <g><path style="opacity:1" fill="#ecedec" d="M 193.5,21.5 C 205.21,19.3781 210.043,24.0448 208,35.5C 201.951,41.6991 195.617,42.0324 189,36.5C 185.923,29.9393 187.423,24.9393 193.5,21.5 Z M 196.5,26.5 C 203.675,28.2145 204.175,31.2145 198,35.5C 193.963,33.1424 193.463,30.1424 196.5,26.5 Z"/></g>
            <g><path style="opacity:1" fill="#ecedec" d="M 211.5,21.5 C 213.833,21.5 216.167,21.5 218.5,21.5C 218.653,27.8692 218.32,34.2026 217.5,40.5C 215.167,40.5 212.833,40.5 210.5,40.5C 211.372,34.2111 211.705,27.8778 211.5,21.5 Z"/></g>
            <g><path style="opacity:1" fill="#ecedec" d="M 222.5,21.5 C 228.361,21.8976 234.361,22.0642 240.5,22C 241.966,23.5522 242.966,25.3855 243.5,27.5C 242.561,31.7649 242.228,36.0983 242.5,40.5C 240.167,40.5 237.833,40.5 235.5,40.5C 235.816,36.4754 235.983,32.4754 236,28.5C 234,25.8333 232,25.8333 230,28.5C 228.844,32.4183 228.344,36.4183 228.5,40.5C 226.167,40.5 223.833,40.5 221.5,40.5C 222.32,34.2026 222.653,27.8692 222.5,21.5 Z"/></g>
            <g><path style="opacity:1" fill="#8cd5f9" d="M 23.5,22.5 C 25.288,22.2148 26.9547,22.5481 28.5,23.5C 28.3764,24.1067 28.0431,24.44 27.5,24.5C 26.1667,23.8333 24.8333,23.1667 23.5,22.5 Z"/></g>
            <g><path style="opacity:1" fill="#7fd1f3" d="M 26.5,26.5 C 28.9479,27.4731 31.6146,27.8064 34.5,27.5C 31.6447,28.8032 28.6447,28.8032 25.5,27.5C 25.6236,26.8933 25.9569,26.56 26.5,26.5 Z"/></g>
            <g><path style="opacity:1" fill="#98d7f4" d="M 21.5,32.5 C 26.6405,33.4881 31.9738,33.8214 37.5,33.5C 31.9871,34.8157 26.3204,34.8157 20.5,33.5C 20.6236,32.8933 20.9569,32.56 21.5,32.5 Z"/></g>
            </svg>
        </div>
        <div class="nb-card">
            <h1 class="nb-heading">登录</h1>
            <p class="nb-subheading">选择您的登录方法</p>
`

	// Add login options to HTML
	for _, opt := range options {
		var iconClass string
		if opt.Local {
			iconClass = "nb-icon-email"
		} else {
			iconClass = "nb-icon-default"
		}

		redirectURL := r.URL.String()
		if strings.HasPrefix(redirectURL, "/oauth2/auth") {
			redirectURL = "/oauth2/auth/" + opt.ID
			if r.URL.RawQuery != "" {
				redirectURL += "?" + r.URL.RawQuery
			}
		}

		html += `<a href="` + redirectURL + `" class="nb-btn-connector">
				<span class="nb-icon ` + iconClass + `"></span>
				<span>` + opt.Name + `</span>
			</a>`
	}

	html += `</div></body></html>`

	// Write response
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func getProviderNameByType(providerType string) string {
	switch providerType {
	case "wechatwork":
		return "企业微信"
	case "oidc":
		return "OIDC"
	case "saml":
		return "SAML"
	case "github":
		return "GitHub"
	case "gitlab":
		return "GitLab"
	case "google":
		return "Google"
	case "microsoft":
		return "Microsoft"
	default:
		return providerType
	}
}
