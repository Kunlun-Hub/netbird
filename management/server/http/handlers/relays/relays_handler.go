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

func AddEndpoints(accountManager account.Manager, config *nbconfig.Relay, router *mux.Router) {
	handler := &Handler{accountManager: accountManager, config: config}
	router.HandleFunc("/relays", handler.getAllRelays).Methods("GET", "OPTIONS")
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
		relays = append(relays, probeRelay(ctx, server, registeredClients))
		seen[relayKey(server.ID, server.Address)] = struct{}{}
	}

	for _, relay := range activeRelayRegistry.list() {
		if _, ok := seen[relayKey(relay.ID, relay.Address)]; ok {
			continue
		}
		relays = append(relays, relay.status(registeredClients))
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
	token, err := signRelaySetupToken(h.config.Secret, expiresAt)
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
	if err := verifyRelaySetupToken(req.SetupKey, h.config.Secret); err != nil {
		util.WriteErrorResponse("invalid relay setup token", http.StatusUnauthorized, w)
		return
	}

	activeRelayRegistry.upsert(registeredRelay{
		ID:               req.ID,
		Name:             req.Name,
		Address:          req.Address,
		ManagementURL:    req.ManagementURL,
		Version:          req.Version,
		ConnectedClients: req.ConnectedClients,
		LastSeen:         time.Now(),
	})

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

	if !activeRelayRegistry.delete(id) {
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

func (r registeredRelay) status(registeredClients int) RelayStatus {
	status := "offline"
	if time.Since(r.LastSeen) <= relayRegistrationTTL {
		status = "online"
	}
	return RelayStatus{
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
}

func relayKey(id, address string) string {
	if id != "" {
		return id
	}
	return address
}

func signRelaySetupToken(secret string, expiresAt time.Time) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload := fmt.Sprintf("%s:%d:%s", relaySetupTokenVersion, expiresAt.Unix(), base64.RawURLEncoding.EncodeToString(nonce))
	sig := relaySetupTokenSignature(secret, payload)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func verifyRelaySetupToken(token, secret string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return errors.New("invalid token format")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return err
	}
	payload := string(payloadBytes)
	if !hmac.Equal(signature, relaySetupTokenSignature(secret, payload)) {
		return errors.New("invalid token signature")
	}
	payloadParts := strings.Split(payload, ":")
	if len(payloadParts) != 3 || payloadParts[0] != relaySetupTokenVersion {
		return errors.New("invalid token payload")
	}
	expiresAt, err := strconv.ParseInt(payloadParts[1], 10, 64)
	if err != nil {
		return err
	}
	if time.Unix(expiresAt, 0).Before(time.Now()) {
		return errors.New("token expired")
	}
	return nil
}

func relaySetupTokenSignature(secret, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func probeRelay(ctx context.Context, server *nbconfig.RelayServer, registeredClients int) RelayStatus {
	result := RelayStatus{
		Address:           server.Address,
		ID:                server.ID,
		Name:              server.Name,
		Status:            "offline",
		RegisteredClients: registeredClients,
		LastChecked:       time.Now(),
	}

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
