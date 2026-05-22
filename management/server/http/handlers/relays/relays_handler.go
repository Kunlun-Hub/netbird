package relays

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/account"
	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/geolocation"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/relay/healthcheck/peerid"
	relayserver "github.com/netbirdio/netbird/relay/server"
	"github.com/netbirdio/netbird/shared/management/http/util"
	nbrelay "github.com/netbirdio/netbird/shared/relay"
	"github.com/netbirdio/netbird/shared/relay/messages"
)

const (
	relayProbeTimeout      = 5 * time.Second
	relaySetupTokenTTL     = 24 * time.Hour
	relayRegistrationTTL   = 2 * time.Minute
	relaySetupTokenVersion = "v1"
)

type Handler struct {
	accountManager account.Manager
	config         *nbconfig.Relay
	geo            geolocation.Geolocation
	configPusher   relayConfigPusher
}

type relayConfigPusher interface {
	PushRelayList(ctx context.Context) int
}

type RelayStatus struct {
	Address           string    `json:"address"`
	ID                string    `json:"id,omitempty"`
	Name              string    `json:"name,omitempty"`
	ObservedID        string    `json:"observed_id,omitempty"`
	Registered        bool      `json:"registered,omitempty"`
	Status            string    `json:"status"`
	ConnectedClients  *int      `json:"connected_clients,omitempty"`
	RegisteredClients int       `json:"registered_clients"`
	PublicIP          string    `json:"public_ip,omitempty"`
	CountryCode       string    `json:"country_code,omitempty"`
	CityName          string    `json:"city_name,omitempty"`
	LastChecked       time.Time `json:"last_checked"`
	Error             string    `json:"error,omitempty"`
}

type relaySetupTokenResponse struct {
	Token           string `json:"token"`
	RelayAuthSecret string `json:"relay_auth_secret"`
	ExpiresAt       string `json:"expires_at"`
}

type registerRelayRequest struct {
	SetupKey         string `json:"setup_key"`
	ID               string `json:"id"`
	Name             string `json:"name,omitempty"`
	Address          string `json:"address"`
	ManagementURL    string `json:"management_url,omitempty"`
	Version          string `json:"version,omitempty"`
	ConnectedClients *int   `json:"connected_clients,omitempty"`
}

type registerRelayResponse struct {
	Status string `json:"status"`
}

type applyRelayConfigResponse struct {
	Status      string `json:"status"`
	TargetPeers int    `json:"target_peers"`
}

type relayPreferencesResponse struct {
	PeerPreferences  map[string][]string `json:"peer_preferences"`
	GroupPreferences map[string][]string `json:"group_preferences"`
}

type healthResponse struct {
	ConnectedPeers *int   `json:"connected_peers,omitempty"`
	RelayID        string `json:"relay_id,omitempty"`
}

type registeredRelay struct {
	ID               string
	Name             string
	Address          string
	ManagementURL    string
	Version          string
	ConnectedClients *int
	LastSeen         time.Time
}

type relayRegistry struct {
	mu     sync.RWMutex
	relays map[string]registeredRelay
}

var activeRelayRegistry = &relayRegistry{
	relays: make(map[string]registeredRelay),
}

func ActiveRelayAddresses(config *nbconfig.Relay) []string {
	return relayAddresses(config, nil, nil)
}

func PreferredRelayAddresses(config *nbconfig.Relay, peerID string, peerGroups []string, settings *types.Settings) []string {
	registeredRelays := registeredRelaysFromSettings(settings)
	allAddresses := relayAddresses(config, nil, registeredRelays)
	preferred := preferredRelayIDs(peerID, peerGroups, settings)
	if len(preferred) == 0 {
		return allAddresses
	}

	addresses := relayAddresses(config, preferred, registeredRelays)
	if len(addresses) == 0 {
		return allAddresses
	}
	return addresses
}

func preferredRelayIDs(peerID string, peerGroups []string, settings *types.Settings) []string {
	if settings == nil || settings.Extra == nil {
		return nil
	}
	if relays := settings.Extra.RelayPeerPreferences[peerID]; len(relays) > 0 {
		return relays
	}
	seen := make(map[string]struct{})
	var relays []string
	for _, groupID := range peerGroups {
		for _, relayID := range settings.Extra.RelayGroupPreferences[groupID] {
			if _, ok := seen[relayID]; ok {
				continue
			}
			relays = append(relays, relayID)
			seen[relayID] = struct{}{}
		}
	}
	return relays
}

func relayAddresses(config *nbconfig.Relay, preferred []string, registeredRelays []registeredRelay) []string {
	relayByPreference := make(map[string]string)
	var allAddresses []string
	seenAll := make(map[string]struct{})
	addRelay := func(id, address string) {
		if address == "" {
			return
		}
		relayByPreference[relayKey(id, address)] = address
		relayByPreference[address] = address
		if _, ok := seenAll[address]; ok {
			return
		}
		allAddresses = append(allAddresses, address)
		seenAll[address] = struct{}{}
	}

	if config != nil {
		for _, server := range config.GetServers() {
			if server == nil {
				continue
			}
			addRelay(server.ID, server.Address)
		}
	}

	for _, relay := range registeredRelays {
		if time.Since(relay.LastSeen) > relayRegistrationTTL {
			continue
		}
		addRelay(relay.ID, relay.Address)
	}

	for _, relay := range activeRelayRegistry.list() {
		if time.Since(relay.LastSeen) > relayRegistrationTTL {
			continue
		}
		addRelay(relay.ID, relay.Address)
	}

	if len(preferred) == 0 {
		return allAddresses
	}

	addresses := make([]string, 0, len(preferred))
	seenPreferred := make(map[string]struct{}, len(preferred))
	for _, relayID := range preferred {
		address := relayByPreference[relayID]
		if address == "" {
			continue
		}
		if _, ok := seenPreferred[address]; ok {
			continue
		}
		addresses = append(addresses, address)
		seenPreferred[address] = struct{}{}
	}
	return addresses
}

func AddEndpoints(accountManager account.Manager, config *nbconfig.Relay, geo geolocation.Geolocation, configPusher relayConfigPusher, router *mux.Router) {
	handler := &Handler{accountManager: accountManager, config: config, geo: geo, configPusher: configPusher}
	router.HandleFunc("/relays", handler.getAllRelays).Methods("GET", "OPTIONS")
	router.HandleFunc("/relays/apply", handler.applyRelayConfig).Methods("POST", "OPTIONS")
	router.HandleFunc("/relays/preferences", handler.getRelayPreferences).Methods("GET", "OPTIONS")
	router.HandleFunc("/relays/preferences", handler.saveRelayPreferences).Methods("PUT", "OPTIONS")
	router.HandleFunc("/relays/setup-token", handler.createSetupToken).Methods("POST", "OPTIONS")
	router.HandleFunc("/relays/register", handler.registerRelay).Methods("POST", "OPTIONS")
	router.HandleFunc("/relays/{id}", handler.deleteRelay).Methods("DELETE", "OPTIONS")
}

func (h *Handler) getAllRelays(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), relayProbeTimeout)
	defer cancel()

	servers := make([]*nbconfig.RelayServer, 0)
	if h.config != nil {
		servers = h.config.GetServers()
	}

	registeredClients, err := h.registeredClients(ctx, userAuth.AccountId, userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	relays := make([]RelayStatus, 0, len(servers))
	seen := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		if server == nil || server.Address == "" {
			continue
		}
		relays = append(relays, h.probeRelay(ctx, server, registeredClients))
		seen[relayKey(server.ID, server.Address)] = struct{}{}
	}

	for _, relay := range activeRelayRegistry.list() {
		if _, ok := seen[relayKey(relay.ID, relay.Address)]; ok {
			continue
		}
		relays = append(relays, h.registeredRelayStatus(relay, registeredClients))
		seen[relayKey(relay.ID, relay.Address)] = struct{}{}
	}

	for _, relay := range h.storedRegisteredRelays(ctx, userAuth.AccountId) {
		if _, ok := seen[relayKey(relay.ID, relay.Address)]; ok {
			continue
		}
		relays = append(relays, h.registeredRelayStatus(relay, registeredClients))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(relays); err != nil {
		log.WithContext(r.Context()).Errorf("failed to encode relay status response: %v", err)
	}
}

func (h *Handler) createSetupToken(w http.ResponseWriter, r *http.Request) {
	if h.config == nil || h.config.Secret == "" {
		util.WriteErrorResponse("relay secret is not configured", http.StatusPreconditionFailed, w)
		return
	}

	expiresAt := time.Now().Add(relaySetupTokenTTL)
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	token, err := signRelaySetupToken(h.config.Secret, expiresAt, userAuth.AccountId)
	if err != nil {
		util.WriteErrorResponse("failed to generate relay setup token", http.StatusInternalServerError, w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(relaySetupTokenResponse{
		Token:           token,
		RelayAuthSecret: h.config.Secret,
		ExpiresAt:       expiresAt.UTC().Format(time.RFC3339),
	}); err != nil {
		log.WithContext(r.Context()).Errorf("failed to encode relay setup token response: %v", err)
	}
}

func (h *Handler) getRelayPreferences(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	settings, err := h.accountManager.GetAccountSettings(r.Context(), userAuth.AccountId, userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	response := relayPreferencesResponse{
		PeerPreferences:  map[string][]string{},
		GroupPreferences: map[string][]string{},
	}
	if settings != nil && settings.Extra != nil {
		response.PeerPreferences = clonePreferences(settings.Extra.RelayPeerPreferences)
		response.GroupPreferences = clonePreferences(settings.Extra.RelayGroupPreferences)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.WithContext(r.Context()).Errorf("failed to encode relay preferences response: %v", err)
	}
}

func (h *Handler) applyRelayConfig(w http.ResponseWriter, r *http.Request) {
	if h.configPusher == nil {
		util.WriteErrorResponse("relay config pusher is not configured", http.StatusPreconditionFailed, w)
		return
	}

	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	if _, err := h.accountManager.GetAccountByID(r.Context(), userAuth.AccountId, userAuth.UserId); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	targetPeers := h.configPusher.PushRelayList(r.Context())

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(applyRelayConfigResponse{
		Status:      "ok",
		TargetPeers: targetPeers,
	}); err != nil {
		log.WithContext(r.Context()).Errorf("failed to encode relay config apply response: %v", err)
	}
}

func (h *Handler) saveRelayPreferences(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	var req relayPreferencesResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}

	settings, err := h.accountManager.GetAccountSettings(r.Context(), userAuth.AccountId, userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	settings = settings.Copy()
	if settings.Extra == nil {
		settings.Extra = &types.ExtraSettings{}
	}
	settings.Extra.RelayPeerPreferences = sanitizePreferences(req.PeerPreferences)
	settings.Extra.RelayGroupPreferences = sanitizePreferences(req.GroupPreferences)
	if err := h.validateRelayPreferences(r.Context(), userAuth.AccountId, userAuth.UserId, settings.Extra.RelayPeerPreferences, settings.Extra.RelayGroupPreferences); err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}

	if _, err := h.accountManager.UpdateAccountSettings(r.Context(), userAuth.AccountId, userAuth.UserId, settings); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	response := relayPreferencesResponse{
		PeerPreferences:  settings.Extra.RelayPeerPreferences,
		GroupPreferences: settings.Extra.RelayGroupPreferences,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.WithContext(r.Context()).Errorf("failed to encode relay preferences response: %v", err)
	}
}

func (h *Handler) registerRelay(w http.ResponseWriter, r *http.Request) {
	if h.config == nil || h.config.Secret == "" {
		util.WriteErrorResponse("relay secret is not configured", http.StatusPreconditionFailed, w)
		return
	}

	var req registerRelayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}

	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	req.Address = strings.TrimSpace(req.Address)
	if req.ID == "" {
		util.WriteErrorResponse("relay ID is required", http.StatusBadRequest, w)
		return
	}
	if req.Address == "" {
		util.WriteErrorResponse("relay address is required", http.StatusBadRequest, w)
		return
	}
	accountID, err := verifyRelaySetupToken(req.SetupKey, h.config.Secret)
	if err != nil {
		util.WriteErrorResponse("invalid relay setup token", http.StatusUnauthorized, w)
		return
	}
	if accountID == "" {
		accountID, err = h.accountManager.GetStore().GetAnyAccountID(r.Context())
		if err != nil {
			log.WithContext(r.Context()).Warnf("relay registration has no account in setup token and no fallback account was found: %v", err)
		}
	}

	relay := registeredRelay{
		ID:               req.ID,
		Name:             req.Name,
		Address:          req.Address,
		ManagementURL:    req.ManagementURL,
		Version:          req.Version,
		ConnectedClients: req.ConnectedClients,
		LastSeen:         time.Now(),
	}
	activeRelayRegistry.upsert(relay)
	if accountID != "" {
		if err := h.persistRegisteredRelay(r.Context(), accountID, relay); err != nil {
			log.WithContext(r.Context()).Warnf("failed to persist registered relay %s for account %s: %v", relay.ID, accountID, err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(registerRelayResponse{Status: "ok"}); err != nil {
		log.WithContext(r.Context()).Errorf("failed to encode relay registration response: %v", err)
	}
}

func (h *Handler) deleteRelay(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		util.WriteErrorResponse("relay ID is required", http.StatusBadRequest, w)
		return
	}

	deletedActive := activeRelayRegistry.delete(id)
	deletedStored := h.deleteStoredRelay(r.Context(), id)
	if !deletedActive && !deletedStored {
		util.WriteErrorResponse("relay not found", http.StatusNotFound, w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) registeredClients(ctx context.Context, accountID, userID string) (int, error) {
	peers, err := h.accountManager.GetPeers(ctx, accountID, userID, "", "")
	if err != nil {
		return 0, err
	}

	count := 0
	for _, peer := range peers {
		if peer.ProxyMeta.Embedded {
			continue
		}
		count++
	}
	return count, nil
}

func (h *Handler) validateRelayPreferences(ctx context.Context, accountID, userID string, peerPreferences, groupPreferences map[string][]string) error {
	validRelays := h.relayPreferenceKeys(ctx, accountID)
	for targetID, relayIDs := range peerPreferences {
		for _, relayID := range relayIDs {
			if _, ok := validRelays[relayID]; !ok {
				return fmt.Errorf("unknown relay %q for peer %q", relayID, targetID)
			}
		}
	}
	for targetID, relayIDs := range groupPreferences {
		for _, relayID := range relayIDs {
			if _, ok := validRelays[relayID]; !ok {
				return fmt.Errorf("unknown relay %q for group %q", relayID, targetID)
			}
		}
	}

	peers, err := h.accountManager.GetPeers(ctx, accountID, userID, "", "")
	if err != nil {
		return err
	}
	validPeers := make(map[string]struct{}, len(peers))
	for _, peer := range peers {
		validPeers[peer.ID] = struct{}{}
	}
	for peerID := range peerPreferences {
		if _, ok := validPeers[peerID]; !ok {
			return fmt.Errorf("unknown peer %q", peerID)
		}
	}

	groups, err := h.accountManager.GetAllGroups(ctx, accountID, userID)
	if err != nil {
		return err
	}
	validGroups := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		validGroups[group.ID] = struct{}{}
	}
	for groupID := range groupPreferences {
		if _, ok := validGroups[groupID]; !ok {
			return fmt.Errorf("unknown group %q", groupID)
		}
	}

	return nil
}

func (h *Handler) relayPreferenceKeys(ctx context.Context, accountID string) map[string]struct{} {
	keys := make(map[string]struct{})
	add := func(id, address string) {
		if id != "" {
			keys[id] = struct{}{}
		}
		if address != "" {
			keys[address] = struct{}{}
		}
		if key := relayKey(id, address); key != "" {
			keys[key] = struct{}{}
		}
	}
	if h.config != nil {
		for _, server := range h.config.GetServers() {
			if server == nil {
				continue
			}
			add(server.ID, server.Address)
		}
	}
	for _, relay := range activeRelayRegistry.list() {
		add(relay.ID, relay.Address)
	}
	for _, relay := range h.storedRegisteredRelays(ctx, accountID) {
		add(relay.ID, relay.Address)
	}
	return keys
}

func clonePreferences(source map[string][]string) map[string][]string {
	if source == nil {
		return map[string][]string{}
	}
	result := make(map[string][]string, len(source))
	for targetID, relayIDs := range source {
		result[targetID] = append([]string(nil), relayIDs...)
	}
	return result
}

func sanitizePreferences(source map[string][]string) map[string][]string {
	result := make(map[string][]string, len(source))
	for targetID, relayIDs := range source {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		seen := make(map[string]struct{}, len(relayIDs))
		for _, relayID := range relayIDs {
			relayID = strings.TrimSpace(relayID)
			if relayID == "" {
				continue
			}
			if _, ok := seen[relayID]; ok {
				continue
			}
			result[targetID] = append(result[targetID], relayID)
			seen[relayID] = struct{}{}
		}
		if len(result[targetID]) == 0 {
			delete(result, targetID)
		}
	}
	return result
}

func (r *relayRegistry) upsert(relay registeredRelay) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.relays[relayKey(relay.ID, relay.Address)] = relay
}

func (r *relayRegistry) delete(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.relays[id]; !ok {
		return false
	}
	delete(r.relays, id)
	return true
}

func (r *relayRegistry) list() []registeredRelay {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]registeredRelay, 0, len(r.relays))
	for _, relay := range r.relays {
		result = append(result, relay)
	}
	return result
}

func (h *Handler) storedRegisteredRelays(ctx context.Context, accountID string) []registeredRelay {
	if accountID == "" || h.accountManager == nil {
		return nil
	}
	settings, err := h.accountManager.GetStore().GetAccountSettings(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		log.WithContext(ctx).Debugf("failed to load stored registered relays for account %s: %v", accountID, err)
		return nil
	}
	return registeredRelaysFromSettings(settings)
}

func registeredRelaysFromSettings(settings *types.Settings) []registeredRelay {
	if settings == nil || settings.Extra == nil || len(settings.Extra.RegisteredRelays) == 0 {
		return nil
	}
	result := make([]registeredRelay, 0, len(settings.Extra.RegisteredRelays))
	for _, relay := range settings.Extra.RegisteredRelays {
		result = append(result, registeredRelay{
			ID:               relay.ID,
			Name:             relay.Name,
			Address:          relay.Address,
			ManagementURL:    relay.ManagementURL,
			Version:          relay.Version,
			ConnectedClients: relay.ConnectedClients,
			LastSeen:         relay.LastSeen,
		})
	}
	return result
}

func (h *Handler) persistRegisteredRelay(ctx context.Context, accountID string, relay registeredRelay) error {
	return h.accountManager.GetStore().ExecuteInTransaction(ctx, func(transaction store.Store) error {
		settings, err := transaction.GetAccountSettings(ctx, store.LockingStrengthUpdate, accountID)
		if err != nil {
			return err
		}
		settings = settings.Copy()
		if settings.Extra == nil {
			settings.Extra = &types.ExtraSettings{}
		}
		if settings.Extra.RegisteredRelays == nil {
			settings.Extra.RegisteredRelays = make(map[string]types.RegisteredRelay)
		}
		settings.Extra.RegisteredRelays[relayKey(relay.ID, relay.Address)] = types.RegisteredRelay{
			ID:               relay.ID,
			Name:             relay.Name,
			Address:          relay.Address,
			ManagementURL:    relay.ManagementURL,
			Version:          relay.Version,
			ConnectedClients: relay.ConnectedClients,
			LastSeen:         relay.LastSeen,
		}
		return transaction.SaveAccountSettings(ctx, accountID, settings)
	})
}

func (h *Handler) deleteStoredRelay(ctx context.Context, id string) bool {
	deleted := false
	for _, account := range h.accountManager.GetStore().GetAllAccounts(ctx) {
		accountID := account.Id
		if err := h.accountManager.GetStore().ExecuteInTransaction(ctx, func(transaction store.Store) error {
			settings, err := transaction.GetAccountSettings(ctx, store.LockingStrengthUpdate, accountID)
			if err != nil {
				return err
			}
			if settings == nil || settings.Extra == nil || len(settings.Extra.RegisteredRelays) == 0 {
				return nil
			}
			if _, ok := settings.Extra.RegisteredRelays[id]; !ok {
				return nil
			}
			settings = settings.Copy()
			delete(settings.Extra.RegisteredRelays, id)
			deleted = true
			return transaction.SaveAccountSettings(ctx, accountID, settings)
		}); err != nil {
			log.WithContext(ctx).Warnf("failed to delete stored relay %s for account %s: %v", id, accountID, err)
		}
	}
	return deleted
}

func (h *Handler) registeredRelayStatus(r registeredRelay, registeredClients int) RelayStatus {
	status := "offline"
	if time.Since(r.LastSeen) <= relayRegistrationTTL {
		status = "online"
	}
	result := RelayStatus{
		Address:           r.Address,
		ID:                r.ID,
		Name:              r.Name,
		ObservedID:        r.ID,
		Registered:        true,
		Status:            status,
		ConnectedClients:  r.ConnectedClients,
		RegisteredClients: registeredClients,
		LastChecked:       r.LastSeen,
	}
	h.enrichRelayLocation(&result)
	return result
}

func relayKey(id, address string) string {
	if id != "" {
		return id
	}
	return address
}

func signRelaySetupToken(secret string, expiresAt time.Time, accountID string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload := fmt.Sprintf("%s:%d:%s:%s", relaySetupTokenVersion, expiresAt.Unix(), base64.RawURLEncoding.EncodeToString(nonce), accountID)
	sig := relaySetupTokenSignature(secret, payload)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func verifyRelaySetupToken(token, secret string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", errors.New("invalid token format")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	payload := string(payloadBytes)
	if !hmac.Equal(signature, relaySetupTokenSignature(secret, payload)) {
		return "", errors.New("invalid token signature")
	}
	payloadParts := strings.Split(payload, ":")
	if (len(payloadParts) != 3 && len(payloadParts) != 4) || payloadParts[0] != relaySetupTokenVersion {
		return "", errors.New("invalid token payload")
	}
	expiresAt, err := strconv.ParseInt(payloadParts[1], 10, 64)
	if err != nil {
		return "", err
	}
	if time.Unix(expiresAt, 0).Before(time.Now()) {
		return "", errors.New("token expired")
	}
	if len(payloadParts) == 4 {
		return payloadParts[3], nil
	}
	return "", nil
}

func relaySetupTokenSignature(secret, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func (h *Handler) probeRelay(ctx context.Context, server *nbconfig.RelayServer, registeredClients int) RelayStatus {
	result := RelayStatus{
		Address:           server.Address,
		ID:                server.ID,
		Name:              server.Name,
		Status:            "offline",
		RegisteredClients: registeredClients,
		LastChecked:       time.Now(),
	}
	h.enrichRelayLocation(&result)

	if err := probeRelayWebsocket(ctx, server.Address); err != nil {
		result.Error = err.Error()
		return result
	}

	result.Status = "online"
	if health, err := fetchHealth(ctx, server.Address); err == nil {
		result.ConnectedClients = health.ConnectedPeers
		result.ObservedID = health.RelayID
	}
	return result
}

func (h *Handler) enrichRelayLocation(result *RelayStatus) {
	ip := publicIPFromAddress(result.Address)
	if ip == nil {
		return
	}
	result.PublicIP = ip.String()
	if h.geo == nil {
		return
	}
	location, err := h.geo.Lookup(ip)
	if err != nil {
		log.Debugf("failed to lookup relay location for %s: %v", ip.String(), err)
		return
	}
	result.CountryCode = location.Country.ISOCode
	result.CityName = location.City.Names.En
}

func publicIPFromAddress(address string) net.IP {
	parsed, err := url.Parse(address)
	if err != nil {
		return nil
	}
	host := parsed.Hostname()
	if host == "" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip
		}
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return nil
}

func probeRelayWebsocket(ctx context.Context, address string) error {
	wsURL, err := relayWebsocketURL(address)
	if err != nil {
		return err
	}

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("connect relay websocket: %w", err)
	}
	defer conn.CloseNow()

	authMsg, err := messages.MarshalAuthMsg(peerid.HealthCheckPeerID, peerid.DummyAuthToken)
	if err != nil {
		return fmt.Errorf("marshal relay health auth: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, authMsg); err != nil {
		return fmt.Errorf("write relay health auth: %w", err)
	}
	return nil
}

func relayWebsocketURL(address string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", fmt.Errorf("parse relay address: %w", err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("relay address has no host")
	}

	switch parsed.Scheme {
	case relayserver.SchemeRELS, "https", "wss":
		parsed.Scheme = "wss"
	default:
		parsed.Scheme = "ws"
	}
	parsed.Path = nbrelay.WebSocketURLPath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func fetchHealth(ctx context.Context, address string) (*healthResponse, error) {
	healthURL, err := relayHealthURL(address)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("relay health returned %s", resp.Status)
	}

	var health healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, err
	}
	return &health, nil
}

func relayHealthURL(address string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("relay address has no host")
	}

	switch parsed.Scheme {
	case relayserver.SchemeRELS, "https", "wss":
		parsed.Scheme = "https"
	default:
		parsed.Scheme = "http"
	}
	parsed.Path = "/health"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
