package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/netip"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/gorilla/mux"

	goversion "github.com/hashicorp/go-version"

	"github.com/netbirdio/netbird/management/server/account"
	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/settings"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/http/api"
	"github.com/netbirdio/netbird/shared/management/http/util"
	"github.com/netbirdio/netbird/shared/management/status"
)

const (
	// PeerBufferPercentage is the percentage of peers to add as buffer for network range calculations
	PeerBufferPercentage = 0.5
	// MinRequiredAddresses is the minimum number of addresses required in a network range
	MinRequiredAddresses = 10
	// MinNetworkBits is the minimum prefix length for IPv4 network ranges (e.g., /29 gives 8 addresses, /28 gives 16)
	MinNetworkBitsIPv4 = 28
	// MinNetworkBitsIPv6 is the minimum prefix length for IPv6 network ranges
	MinNetworkBitsIPv6 = 120
	// MaxNetworkSizeIPv6 is the largest allowed IPv6 prefix (smallest number)
	MaxNetworkSizeIPv6      = 48
	disableAutoUpdate       = "disabled"
	autoUpdateLatestVersion = "latest"
)

// handler is a handler that handles the server.Account HTTP endpoints
type handler struct {
	accountManager  account.Manager
	settingsManager settings.Manager
}

type entitlementsAccountManager interface {
	GetAccountEntitlements(ctx context.Context, accountID, userID string) (*entitlements.Entitlements, error)
}

type accountEntitlementsResponse struct {
	AccountID string          `json:"account_id"`
	Plan      string          `json:"plan"`
	Features  map[string]bool `json:"features"`
	Limits    map[string]int  `json:"limits"`
}

type accountFlowSettingsCompat struct {
	Enabled                                 *bool    `json:"enabled"`
	FlowEnabled                             *bool    `json:"flow_enabled"`
	FlowLogsEnabled                         *bool    `json:"flow_logs_enabled"`
	NetworkTrafficLogsEnabled               *bool    `json:"network_traffic_logs_enabled"`
	Groups                                  []string `json:"groups"`
	FlowGroups                              []string `json:"flow_groups"`
	FlowLogsGroups                          []string `json:"flow_logs_groups"`
	NetworkTrafficLogsGroups                []string `json:"network_traffic_logs_groups"`
	Counters                                *bool    `json:"counters"`
	FlowPacketCounterEnabled                *bool    `json:"flow_packet_counter_enabled"`
	NetworkTrafficPacketCounterEnabled      *bool    `json:"network_traffic_packet_counter_enabled"`
	ExitNodeCollection                      *bool    `json:"exit_node_collection"`
	FlowExitNodeCollectionEnabled           *bool    `json:"flow_exit_node_collection_enabled"`
	NetworkTrafficExitNodeCollectionEnabled *bool    `json:"network_traffic_exit_node_collection_enabled"`
	DnsCollection                           *bool    `json:"dns_collection"`
	FlowDnsCollectionEnabled                *bool    `json:"flow_dns_collection_enabled"`
	NetworkTrafficDnsCollectionEnabled      *bool    `json:"network_traffic_dns_collection_enabled"`
}

type accountSettingsCompatRequest struct {
	Settings struct {
		Extra    *accountFlowSettingsCompat `json:"extra"`
		Flow     *accountFlowSettingsCompat `json:"flow"`
		FlowLogs *accountFlowSettingsCompat `json:"flow_logs"`
	} `json:"settings"`
}

type accountResponseCompat struct {
	*api.Account
}

func (a accountResponseCompat) MarshalJSON() ([]byte, error) {
	if a.Account == nil {
		return json.Marshal(nil)
	}

	type alias api.Account
	payload := map[string]any{}
	raw, err := json.Marshal((*alias)(a.Account))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	settingsMap, _ := payload["settings"].(map[string]any)
	if settingsMap == nil {
		settingsMap = map[string]any{}
		payload["settings"] = settingsMap
	}

	flowMap := buildFlowCompatMap(a.Account.Settings.Extra)
	settingsMap["flow"] = flowMap
	settingsMap["flow_logs"] = flowMap

	return json.Marshal(payload)
}

func buildFlowCompatMap(extra *api.AccountExtraSettings) map[string]any {
	if extra == nil {
		return map[string]any{
			"enabled":              false,
			"flow_enabled":         false,
			"flow_logs_enabled":    false,
			"counters":             false,
			"dns_collection":       false,
			"exit_node_collection": false,
			"groups":               []string{},
			"flow_groups":          []string{},
			"flow_logs_groups":     []string{},
		}
	}

	return map[string]any{
		"enabled":                                      extra.NetworkTrafficLogsEnabled,
		"flow_enabled":                                 boolOrDefault(extra.FlowEnabled, extra.NetworkTrafficLogsEnabled),
		"flow_logs_enabled":                            boolOrDefault(extra.FlowLogsEnabled, extra.NetworkTrafficLogsEnabled),
		"network_traffic_logs_enabled":                 extra.NetworkTrafficLogsEnabled,
		"counters":                                     boolOrDefault(extra.Counters, extra.NetworkTrafficPacketCounterEnabled),
		"flow_packet_counter_enabled":                  boolOrDefault(extra.FlowPacketCounterEnabled, extra.NetworkTrafficPacketCounterEnabled),
		"network_traffic_packet_counter_enabled":       extra.NetworkTrafficPacketCounterEnabled,
		"dns_collection":                               boolOrDefault(extra.DnsCollection, extra.NetworkTrafficDnsCollectionEnabled),
		"flow_dns_collection_enabled":                  boolOrDefault(extra.FlowDnsCollectionEnabled, extra.NetworkTrafficDnsCollectionEnabled),
		"network_traffic_dns_collection_enabled":       extra.NetworkTrafficDnsCollectionEnabled,
		"exit_node_collection":                         boolOrDefault(extra.ExitNodeCollection, extra.NetworkTrafficExitNodeCollectionEnabled),
		"flow_exit_node_collection_enabled":            boolOrDefault(extra.FlowExitNodeCollectionEnabled, extra.NetworkTrafficExitNodeCollectionEnabled),
		"network_traffic_exit_node_collection_enabled": extra.NetworkTrafficExitNodeCollectionEnabled,
		"groups":                      stringsOrDefault(extra.Groups, extra.NetworkTrafficLogsGroups),
		"flow_groups":                 stringsOrDefault(extra.FlowGroups, extra.NetworkTrafficLogsGroups),
		"flow_logs_groups":            stringsOrDefault(extra.FlowLogsGroups, extra.NetworkTrafficLogsGroups),
		"network_traffic_logs_groups": extra.NetworkTrafficLogsGroups,
	}
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func stringsOrDefault(value *[]string, fallback []string) []string {
	if value == nil {
		return fallback
	}
	return *value
}

func AddEndpoints(accountManager account.Manager, settingsManager settings.Manager, router *mux.Router) {
	accountsHandler := newHandler(accountManager, settingsManager)
	router.HandleFunc("/accounts/{accountId}/entitlements", accountsHandler.getAccountEntitlements).Methods("GET", "OPTIONS")
	router.HandleFunc("/accounts/{accountId}", accountsHandler.updateAccount).Methods("PUT", "OPTIONS")
	router.HandleFunc("/accounts/{accountId}", accountsHandler.deleteAccount).Methods("DELETE", "OPTIONS")
	router.HandleFunc("/accounts", accountsHandler.getAllAccounts).Methods("GET", "OPTIONS")
}

// newHandler creates a new handler HTTP handler
func newHandler(accountManager account.Manager, settingsManager settings.Manager) *handler {
	return &handler{
		accountManager:  accountManager,
		settingsManager: settingsManager,
	}
}

func validateIPAddress(addr netip.Addr) error {
	if addr.IsLoopback() {
		return status.Errorf(status.InvalidArgument, "loopback address range not allowed")
	}

	if addr.IsMulticast() {
		return status.Errorf(status.InvalidArgument, "multicast address range not allowed")
	}

	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return status.Errorf(status.InvalidArgument, "link-local address range not allowed")
	}

	return nil
}

func validateMinimumSize(prefix netip.Prefix) error {
	addr := prefix.Addr()
	if addr.Is4() && prefix.Bits() > MinNetworkBitsIPv4 {
		return status.Errorf(status.InvalidArgument, "network range too small: minimum size is /%d for IPv4", MinNetworkBitsIPv4)
	}
	if addr.Is6() {
		if prefix.Bits() > MinNetworkBitsIPv6 {
			return status.Errorf(status.InvalidArgument, "network range too small: minimum size is /%d for IPv6", MinNetworkBitsIPv6)
		}
		if prefix.Bits() < MaxNetworkSizeIPv6 {
			return status.Errorf(status.InvalidArgument, "network range too large: maximum size is /%d for IPv6", MaxNetworkSizeIPv6)
		}
	}
	return nil
}

func (h *handler) parseAndValidateNetworkRange(ctx context.Context, accountID, userID, rangeStr string, requireV6 bool) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(rangeStr)
	if err != nil {
		return netip.Prefix{}, status.Errorf(status.InvalidArgument, "invalid CIDR format: %v", err)
	}
	prefix = prefix.Masked()
	if requireV6 && !prefix.Addr().Is6() {
		return netip.Prefix{}, status.Errorf(status.InvalidArgument, "network range must be an IPv6 address")
	}
	if !requireV6 && prefix.Addr().Is6() {
		return netip.Prefix{}, status.Errorf(status.InvalidArgument, "network range must be an IPv4 address")
	}
	if err := h.validateNetworkRange(ctx, accountID, userID, prefix); err != nil {
		return netip.Prefix{}, err
	}
	return prefix, nil
}

func (h *handler) validateNetworkRange(ctx context.Context, accountID, userID string, networkRange netip.Prefix) error {
	if !networkRange.IsValid() {
		return nil
	}

	if err := validateIPAddress(networkRange.Addr()); err != nil {
		return err
	}

	if err := validateMinimumSize(networkRange); err != nil {
		return err
	}

	return h.validateCapacity(ctx, accountID, userID, networkRange)
}

func (h *handler) validateCapacity(ctx context.Context, accountID, userID string, prefix netip.Prefix) error {
	peers, err := h.accountManager.GetPeers(ctx, accountID, userID, "", "")
	if err != nil {
		return status.Errorf(status.Internal, "get peer count: %v", err)
	}

	maxHosts := calculateMaxHosts(prefix)
	requiredAddresses := calculateRequiredAddresses(len(peers))

	if maxHosts < requiredAddresses {
		return status.Errorf(status.InvalidArgument,
			"network range too small: need at least %d addresses for %d peers + buffer, but range provides %d",
			requiredAddresses, len(peers), maxHosts)
	}

	return nil
}

func calculateMaxHosts(prefix netip.Prefix) int64 {
	hostBits := prefix.Addr().BitLen() - prefix.Bits()
	if hostBits >= 63 {
		return math.MaxInt64
	}

	maxHosts := int64(1) << hostBits
	if prefix.Addr().Is4() {
		maxHosts -= 2 // network and broadcast addresses
	}

	return maxHosts
}

func calculateRequiredAddresses(peerCount int) int64 {
	requiredAddresses := int64(peerCount) + int64(float64(peerCount)*PeerBufferPercentage)
	if requiredAddresses < MinRequiredAddresses {
		requiredAddresses = MinRequiredAddresses
	}
	return requiredAddresses
}

func (h *handler) getAccountEntitlements(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	vars := mux.Vars(r)
	accountID := vars["accountId"]
	if len(accountID) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid account ID"), w)
		return
	}

	entitlementsManager, ok := h.accountManager.(entitlementsAccountManager)
	if !ok {
		util.WriteError(r.Context(), status.Errorf(status.Internal, "account entitlements are not available"), w)
		return
	}

	snapshot, err := entitlementsManager.GetAccountEntitlements(r.Context(), accountID, userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	util.WriteJSONObject(r.Context(), w, toAccountEntitlementsResponse(snapshot))
}

func toAccountEntitlementsResponse(snapshot *entitlements.Entitlements) accountEntitlementsResponse {
	if snapshot == nil {
		return accountEntitlementsResponse{
			Features: map[string]bool{},
			Limits:   map[string]int{},
		}
	}

	features := make(map[string]bool, len(snapshot.Features))
	for feature, enabled := range snapshot.Features {
		features[string(feature)] = enabled
	}

	limits := make(map[string]int, len(snapshot.Limits))
	for limit, value := range snapshot.Limits {
		limits[string(limit)] = value
	}

	return accountEntitlementsResponse{
		AccountID: snapshot.AccountID,
		Plan:      string(snapshot.Plan),
		Features:  features,
		Limits:    limits,
	}
}

// getAllAccounts is HTTP GET handler that returns a list of accounts. Effectively returns just a single account.
func (h *handler) getAllAccounts(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId

	meta, err := h.accountManager.GetAccountMeta(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	settings, err := h.settingsManager.GetSettings(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	onboarding, err := h.accountManager.GetAccountOnboarding(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	resp := toAccountResponse(accountID, settings, meta, onboarding)

	// Populate effective network ranges when settings don't have explicit overrides.
	if resp.Settings.NetworkRange == nil || resp.Settings.NetworkRangeV6 == nil {
		v4, v6, err := h.settingsManager.GetEffectiveNetworkRanges(r.Context(), accountID)
		if err != nil {
			log.WithContext(r.Context()).Warnf("get effective network ranges: %v", err)
		} else {
			if resp.Settings.NetworkRange == nil && v4.IsValid() {
				s := v4.String()
				resp.Settings.NetworkRange = &s
			}
			if resp.Settings.NetworkRangeV6 == nil && v6.IsValid() {
				s := v6.String()
				resp.Settings.NetworkRangeV6 = &s
			}
		}
	}

	util.WriteJSONObject(r.Context(), w, []accountResponseCompat{{Account: resp}})
}

func (h *handler) updateAccountRequestSettings(req api.PutApiAccountsAccountIdJSONRequestBody) (*types.Settings, error) {
	if req.Settings.PeerExposeEnabled && len(req.Settings.PeerExposeGroups) == 0 {
		return nil, status.Errorf(status.InvalidArgument, "peer expose requires at least one group")
	}

	returnSettings := &types.Settings{
		PeerLoginExpirationEnabled: req.Settings.PeerLoginExpirationEnabled,
		PeerLoginExpiration:        time.Duration(float64(time.Second.Nanoseconds()) * float64(req.Settings.PeerLoginExpiration)),
		RegularUsersViewBlocked:    req.Settings.RegularUsersViewBlocked,

		PeerInactivityExpirationEnabled: req.Settings.PeerInactivityExpirationEnabled,
		PeerInactivityExpiration:        time.Duration(float64(time.Second.Nanoseconds()) * float64(req.Settings.PeerInactivityExpiration)),

		PeerExposeEnabled: req.Settings.PeerExposeEnabled,
		PeerExposeGroups:  req.Settings.PeerExposeGroups,
	}

	if req.Settings.Extra != nil {
		returnSettings.Extra = &types.ExtraSettings{
			PeerApprovalEnabled:      req.Settings.Extra.PeerApprovalEnabled,
			UserApprovalRequired:     req.Settings.Extra.UserApprovalRequired,
			FlowEnabled:              req.Settings.Extra.NetworkTrafficLogsEnabled,
			FlowGroups:               req.Settings.Extra.NetworkTrafficLogsGroups,
			FlowPacketCounterEnabled: req.Settings.Extra.NetworkTrafficPacketCounterEnabled,
			FlowENCollectionEnabled:  req.Settings.Extra.NetworkTrafficExitNodeCollectionEnabled,
			FlowDnsCollectionEnabled: req.Settings.Extra.NetworkTrafficDnsCollectionEnabled,
			BrandingLogoDataURL:      stringValue(req.Settings.Extra.BrandingLogoDataUrl),
			BrandingLogoDarkDataURL:  stringValue(req.Settings.Extra.BrandingLogoDarkDataUrl),
			BrandingIconDataURL:      stringValue(req.Settings.Extra.BrandingIconDataUrl),
			BrandingTabTitle:         stringValue(req.Settings.Extra.BrandingTabTitle),
			BrandingPrimaryColor:     stringValue(req.Settings.Extra.BrandingPrimaryColor),
		}
	}

	if req.Settings.JwtGroupsEnabled != nil {
		returnSettings.JWTGroupsEnabled = *req.Settings.JwtGroupsEnabled
	}
	if req.Settings.GroupsPropagationEnabled != nil {
		returnSettings.GroupsPropagationEnabled = *req.Settings.GroupsPropagationEnabled
	}
	if req.Settings.JwtGroupsClaimName != nil {
		returnSettings.JWTGroupsClaimName = *req.Settings.JwtGroupsClaimName
	}
	if req.Settings.JwtAllowGroups != nil {
		returnSettings.JWTAllowGroups = *req.Settings.JwtAllowGroups
	}
	if req.Settings.RoutingPeerDnsResolutionEnabled != nil {
		returnSettings.RoutingPeerDNSResolutionEnabled = *req.Settings.RoutingPeerDnsResolutionEnabled
	}
	if req.Settings.DnsDomain != nil {
		returnSettings.DNSDomain = *req.Settings.DnsDomain
	}
	if req.Settings.LazyConnectionEnabled != nil {
		returnSettings.LazyConnectionEnabled = *req.Settings.LazyConnectionEnabled
	}
	if req.Settings.AutoUpdateVersion != nil {
		_, err := goversion.NewSemver(*req.Settings.AutoUpdateVersion)
		if *req.Settings.AutoUpdateVersion == autoUpdateLatestVersion ||
			*req.Settings.AutoUpdateVersion == disableAutoUpdate ||
			err == nil {
			returnSettings.AutoUpdateVersion = *req.Settings.AutoUpdateVersion
		} else if *req.Settings.AutoUpdateVersion != "" {
			return nil, fmt.Errorf("invalid AutoUpdateVersion")
		}
	}
	if req.Settings.AutoUpdateAlways != nil {
		returnSettings.AutoUpdateAlways = *req.Settings.AutoUpdateAlways
	}
	if req.Settings.LoginMethod != nil {
		returnSettings.LoginMethod = string(*req.Settings.LoginMethod)
	}
	if req.Settings.EnabledLoginOptions != nil {
		enabledLoginOptions := make([]types.LoginOption, len(*req.Settings.EnabledLoginOptions))
		for i, opt := range *req.Settings.EnabledLoginOptions {
			enabledLoginOptions[i] = types.LoginOption(opt)
		}
		returnSettings.EnabledLoginOptions = enabledLoginOptions
		returnSettings.EnabledLoginOptionsSet = true
	}
	if req.Settings.LocalMfaEnabled != nil {
		returnSettings.LocalMfaEnabled = *req.Settings.LocalMfaEnabled
	}
	if req.Settings.Ipv6EnabledGroups != nil {
		returnSettings.IPv6EnabledGroups = *req.Settings.Ipv6EnabledGroups
	}

	return returnSettings, nil
}

func applyFlowCompatSettings(settings *types.Settings, compat *accountSettingsCompatRequest) {
	if settings == nil || compat == nil {
		return
	}

	sources := []*accountFlowSettingsCompat{
		compat.Settings.Extra,
		compat.Settings.Flow,
		compat.Settings.FlowLogs,
	}

	var hasFlowSettings bool
	for _, source := range sources {
		if source != nil {
			hasFlowSettings = true
			break
		}
	}
	if !hasFlowSettings {
		return
	}

	if settings.Extra == nil {
		settings.Extra = &types.ExtraSettings{}
	}

	for _, source := range sources {
		if source == nil {
			continue
		}

		if v := firstBool(source.Enabled, source.FlowEnabled, source.FlowLogsEnabled, source.NetworkTrafficLogsEnabled); v != nil {
			settings.Extra.FlowEnabled = *v
		}
		if v := firstBool(source.Counters, source.FlowPacketCounterEnabled, source.NetworkTrafficPacketCounterEnabled); v != nil {
			settings.Extra.FlowPacketCounterEnabled = *v
		}
		if v := firstBool(source.ExitNodeCollection, source.FlowExitNodeCollectionEnabled, source.NetworkTrafficExitNodeCollectionEnabled); v != nil {
			settings.Extra.FlowENCollectionEnabled = *v
		}
		if v := firstBool(source.DnsCollection, source.FlowDnsCollectionEnabled, source.NetworkTrafficDnsCollectionEnabled); v != nil {
			settings.Extra.FlowDnsCollectionEnabled = *v
		}
		if groups := firstStrings(source.Groups, source.FlowGroups, source.FlowLogsGroups, source.NetworkTrafficLogsGroups); groups != nil {
			settings.Extra.FlowGroups = groups
		}
	}
}

func firstBool(values ...*bool) *bool {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstStrings(values ...[]string) []string {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalBool(value bool) *bool {
	if !value {
		return nil
	}
	return &value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalStrings(value []string) *[]string {
	if len(value) == 0 {
		return nil
	}
	return &value
}

// updateAccount is HTTP PUT handler that updates the provided account. Updates only account settings (server.Settings)
func (h *handler) updateAccount(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	_, userID := userAuth.AccountId, userAuth.UserId

	vars := mux.Vars(r)
	accountID := vars["accountId"]
	if len(accountID) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid accountID ID"), w)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		util.WriteErrorResponse("couldn't read JSON request", http.StatusBadRequest, w)
		return
	}

	var req api.PutApiAccountsAccountIdJSONRequestBody
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&req)
	if err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}

	var compat accountSettingsCompatRequest
	if err := json.Unmarshal(body, &compat); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}

	settings, err := h.updateAccountRequestSettings(req)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	applyFlowCompatSettings(settings, &compat)
	if req.Settings.NetworkRange != nil && *req.Settings.NetworkRange != "" {
		prefix, err := h.parseAndValidateNetworkRange(r.Context(), accountID, userID, *req.Settings.NetworkRange, false)
		if err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}
		settings.NetworkRange = prefix
	}

	if req.Settings.NetworkRangeV6 != nil && *req.Settings.NetworkRangeV6 != "" {
		prefix, err := h.parseAndValidateNetworkRange(r.Context(), accountID, userID, *req.Settings.NetworkRangeV6, true)
		if err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}
		settings.NetworkRangeV6 = prefix
	}

	var onboarding *types.AccountOnboarding
	if req.Onboarding != nil {
		onboarding = &types.AccountOnboarding{
			OnboardingFlowPending: req.Onboarding.OnboardingFlowPending,
			SignupFormPending:     req.Onboarding.SignupFormPending,
		}
	}

	updatedOnboarding, err := h.accountManager.UpdateAccountOnboarding(r.Context(), accountID, userID, onboarding)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	updatedSettings, err := h.accountManager.UpdateAccountSettings(r.Context(), accountID, userID, settings)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	meta, err := h.accountManager.GetAccountMeta(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	resp := toAccountResponse(accountID, updatedSettings, meta, updatedOnboarding)
	util.WriteJSONObject(r.Context(), w, accountResponseCompat{Account: resp})
}

// deleteAccount is a HTTP DELETE handler to delete an account
func (h *handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	vars := mux.Vars(r)
	targetAccountID := vars["accountId"]
	if len(targetAccountID) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid account ID"), w)
		return
	}

	err = h.accountManager.DeleteAccount(r.Context(), targetAccountID, userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	util.WriteJSONObject(r.Context(), w, util.EmptyObject{})
}

func toAccountResponse(accountID string, settings *types.Settings, meta *types.AccountMeta, onboarding *types.AccountOnboarding) *api.Account {
	jwtAllowGroups := settings.JWTAllowGroups
	if jwtAllowGroups == nil {
		jwtAllowGroups = []string{}
	}
	loginMethod := api.AccountSettingsLoginMethod(settings.LoginMethod)
	if loginMethod == "" {
		loginMethod = api.AccountSettingsLoginMethodAll
	}

	var enabledLoginOptions *[]string
	if len(settings.EnabledLoginOptions) > 0 {
		options := make([]string, len(settings.EnabledLoginOptions))
		for i, opt := range settings.EnabledLoginOptions {
			options[i] = string(opt)
		}
		enabledLoginOptions = &options
	}

	apiSettings := api.AccountSettings{
		PeerLoginExpiration:             int(settings.PeerLoginExpiration.Seconds()),
		PeerLoginExpirationEnabled:      settings.PeerLoginExpirationEnabled,
		PeerInactivityExpiration:        int(settings.PeerInactivityExpiration.Seconds()),
		PeerInactivityExpirationEnabled: settings.PeerInactivityExpirationEnabled,
		GroupsPropagationEnabled:        &settings.GroupsPropagationEnabled,
		JwtGroupsEnabled:                &settings.JWTGroupsEnabled,
		JwtGroupsClaimName:              &settings.JWTGroupsClaimName,
		JwtAllowGroups:                  &jwtAllowGroups,
		RegularUsersViewBlocked:         settings.RegularUsersViewBlocked,
		RoutingPeerDnsResolutionEnabled: &settings.RoutingPeerDNSResolutionEnabled,
		PeerExposeEnabled:               settings.PeerExposeEnabled,
		PeerExposeGroups:                settings.PeerExposeGroups,
		LazyConnectionEnabled:           &settings.LazyConnectionEnabled,
		DnsDomain:                       &settings.DNSDomain,
		AutoUpdateVersion:               &settings.AutoUpdateVersion,
		AutoUpdateAlways:                &settings.AutoUpdateAlways,
		Ipv6EnabledGroups:               &settings.IPv6EnabledGroups,
		EmbeddedIdpEnabled:              &settings.EmbeddedIdpEnabled,
		LocalAuthDisabled:               &settings.LocalAuthDisabled,
		LoginMethod:                     &loginMethod,
		EnabledLoginOptions:             enabledLoginOptions,
		LocalMfaEnabled:                 &settings.LocalMfaEnabled,
	}

	if settings.NetworkRange.IsValid() {
		networkRangeStr := settings.NetworkRange.String()
		apiSettings.NetworkRange = &networkRangeStr
	}
	if settings.NetworkRangeV6.IsValid() {
		networkRangeV6Str := settings.NetworkRangeV6.String()
		apiSettings.NetworkRangeV6 = &networkRangeV6Str
	}

	apiOnboarding := api.AccountOnboarding{
		OnboardingFlowPending: onboarding.OnboardingFlowPending,
		SignupFormPending:     onboarding.SignupFormPending,
	}

	if settings.Extra != nil {
		apiSettings.Extra = &api.AccountExtraSettings{
			Enabled:                                 optionalBool(settings.Extra.FlowEnabled),
			Counters:                                optionalBool(settings.Extra.FlowPacketCounterEnabled),
			DnsCollection:                           optionalBool(settings.Extra.FlowDnsCollectionEnabled),
			ExitNodeCollection:                      optionalBool(settings.Extra.FlowENCollectionEnabled),
			FlowEnabled:                             optionalBool(settings.Extra.FlowEnabled),
			FlowGroups:                              optionalStrings(settings.Extra.FlowGroups),
			FlowLogsEnabled:                         optionalBool(settings.Extra.FlowEnabled),
			FlowLogsGroups:                          optionalStrings(settings.Extra.FlowGroups),
			FlowDnsCollectionEnabled:                optionalBool(settings.Extra.FlowDnsCollectionEnabled),
			FlowExitNodeCollectionEnabled:           optionalBool(settings.Extra.FlowENCollectionEnabled),
			FlowPacketCounterEnabled:                optionalBool(settings.Extra.FlowPacketCounterEnabled),
			Groups:                                  optionalStrings(settings.Extra.FlowGroups),
			PeerApprovalEnabled:                     settings.Extra.PeerApprovalEnabled,
			UserApprovalRequired:                    settings.Extra.UserApprovalRequired,
			NetworkTrafficLogsEnabled:               settings.Extra.FlowEnabled,
			NetworkTrafficLogsGroups:                settings.Extra.FlowGroups,
			NetworkTrafficPacketCounterEnabled:      settings.Extra.FlowPacketCounterEnabled,
			NetworkTrafficExitNodeCollectionEnabled: settings.Extra.FlowENCollectionEnabled,
			NetworkTrafficDnsCollectionEnabled:      settings.Extra.FlowDnsCollectionEnabled,
			BrandingLogoDataUrl:                     optionalString(settings.Extra.BrandingLogoDataURL),
			BrandingLogoDarkDataUrl:                 optionalString(settings.Extra.BrandingLogoDarkDataURL),
			BrandingIconDataUrl:                     optionalString(settings.Extra.BrandingIconDataURL),
			BrandingTabTitle:                        optionalString(settings.Extra.BrandingTabTitle),
			BrandingPrimaryColor:                    optionalString(settings.Extra.BrandingPrimaryColor),
		}
	}

	return &api.Account{
		Id:             accountID,
		Settings:       apiSettings,
		CreatedAt:      meta.CreatedAt,
		CreatedBy:      meta.CreatedBy,
		Domain:         meta.Domain,
		DomainCategory: meta.DomainCategory,
		Onboarding:     apiOnboarding,
	}
}
