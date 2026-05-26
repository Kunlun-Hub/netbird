package client

import (
	"container/list"
	"context"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	relayAuth "github.com/netbirdio/netbird/shared/relay/auth/hmac"
)

var (
	relayCleanupInterval    = 60 * time.Second
	keepUnusedServerTime    = 5 * time.Second
	preferredReconcileDelay = 5 * time.Second
	preferredProbeTimeout   = 10 * time.Second
	relayProbeTimeout       = 6 * time.Second
	defaultRelayWeight      = 30
	preferredRelayWeight    = 70

	ErrRelayClientNotConnected = fmt.Errorf("relay client not connected")
)

// RelayTrack hold the relay clients for the foreign relay servers.
// With the mutex can ensure we can open new connection in case the relay connection has been established with
// the relay server.
type RelayTrack struct {
	sync.RWMutex
	relayClient *Client
	err         error
	created     time.Time
}

func NewRelayTrack() *RelayTrack {
	return &RelayTrack{
		created: time.Now(),
	}
}

type OnServerCloseListener func()

type RelayServerInfo struct {
	URL       string
	Weight    int
	Preferred bool
	Forced    bool
	Current   bool
	Available bool
	Error     string
}

// ManagerOption configures a Manager at construction time.
type ManagerOption func(*Manager)

// WithMaxBackoffInterval caps the exponential backoff between reconnect
// attempts to the home relay. A non-positive value keeps the default.
func WithMaxBackoffInterval(d time.Duration) ManagerOption {
	return func(m *Manager) { m.maxBackoffInterval = d }
}

// Manager is a manager for the relay client instances. It establishes one persistent connection to the given relay URL
// and automatically reconnect to them in case disconnection.
// The manager also manage temporary relay connection. If a client wants to communicate with a client on a
// different relay server, the manager will establish a new connection to the relay server. The connection with these
// relay servers will be closed if there is no active connection. Periodically the manager will check if there is any
// unused relay connection and close it.
type Manager struct {
	ctx          context.Context
	peerID       string
	running      bool
	tokenStore   *relayAuth.TokenStore
	serverPicker *ServerPicker

	relayClient *Client
	// the guard logic can overwrite the relayClient variable, this mutex protect the usage of the variable
	relayClientMu  sync.RWMutex
	reconnectGuard *Guard

	relayClients      map[string]*RelayTrack
	relayClientsMutex sync.RWMutex

	onDisconnectedListeners map[string]*list.List
	onReconnectedListenerFn func()
	listenerLock            sync.Mutex

	mtu                 uint16
	maxBackoffInterval  time.Duration
	switchMu            sync.Mutex
	relayConfigMu       sync.RWMutex
	configuredRelayURLs []string
	relayWeights        map[string]int
	preferredRelays     map[string]struct{}
	forcedRelayURL      string

	cleanupInterval      time.Duration
	keepUnusedServerTime time.Duration
}

// NewManager creates a new manager instance.
// The serverURL address can be empty. In this case, the manager will not serve.
func NewManager(ctx context.Context, serverURLs []string, peerID string, mtu uint16, opts ...ManagerOption) *Manager {
	tokenStore := &relayAuth.TokenStore{}

	m := &Manager{
		ctx:        ctx,
		peerID:     peerID,
		tokenStore: tokenStore,
		mtu:        mtu,
		serverPicker: &ServerPicker{
			TokenStore:        tokenStore,
			PeerID:            peerID,
			MTU:               mtu,
			ConnectionTimeout: defaultConnectionTimeout,
		},
		relayClients:            make(map[string]*RelayTrack),
		onDisconnectedListeners: make(map[string]*list.List),
		cleanupInterval:         relayCleanupInterval,
		keepUnusedServerTime:    keepUnusedServerTime,
	}
	for _, opt := range opts {
		opt(m)
	}
	m.configuredRelayURLs = slices.Clone(serverURLs)
	m.relayWeights = relayWeightsFromURLs(serverURLs)
	m.preferredRelays = legacyPreferredRelays(serverURLs)
	m.serverPicker.ServerURLs.Store(m.effectiveRelayURLsLocked())
	m.reconnectGuard = NewGuard(m.serverPicker, m.maxBackoffInterval)
	return m
}

// Serve starts the manager, attempting to establish a connection with the relay server.
// If the connection fails, it will keep trying to reconnect in the background.
// Additionally, it starts a cleanup loop to remove unused relay connections.
// The manager will automatically reconnect to the relay server in case of disconnection.
func (m *Manager) Serve() error {
	if m.running {
		return fmt.Errorf("manager already serving")
	}
	m.running = true
	log.Debugf("starting relay client manager with %v relay servers", m.serverPicker.ServerURLs.Load())

	client, err := m.serverPicker.PickServer(m.ctx)
	if err != nil {
		go m.reconnectGuard.StartReconnectTrys(m.ctx, nil)
	} else {
		m.storeClient(client)
		m.schedulePreferredHomeRelayReconcile()
	}

	go m.listenGuardEvent(m.ctx)
	go m.startCleanupLoop()
	return err
}

// OpenConn opens a connection to the given peer key. If the peer is on the same relay server, the connection will be
// established via the relay server. If the peer is on a different relay server, the manager will establish a new
// connection to the relay server. It returns back with a net.Conn what represent the remote peer connection.
//
// serverIP, when valid and serverAddress is foreign, is used as a dial target if the FQDN-based dial fails.
// Ignored for the local home-server path. TLS verification still uses the FQDN via SNI.
func (m *Manager) OpenConn(ctx context.Context, serverAddress, peerKey string, serverIP netip.Addr) (net.Conn, error) {
	m.relayClientMu.RLock()
	defer m.relayClientMu.RUnlock()

	if m.relayClient == nil {
		return nil, ErrRelayClientNotConnected
	}

	foreign, err := m.isForeignServer(serverAddress)
	if err != nil {
		return nil, err
	}

	var (
		netConn net.Conn
	)
	if !foreign {
		log.Debugf("open peer connection via permanent server: %s", peerKey)
		netConn, err = m.relayClient.OpenConn(ctx, peerKey)
	} else {
		log.Debugf("open peer connection via foreign server: %s", serverAddress)
		netConn, err = m.openConnVia(ctx, serverAddress, peerKey, serverIP)
	}
	if err != nil {
		return nil, err
	}

	return netConn, err
}

// Ready returns true if the home Relay client is connected to the relay server.
func (m *Manager) Ready() bool {
	m.relayClientMu.RLock()
	defer m.relayClientMu.RUnlock()

	if m.relayClient == nil {
		return false
	}
	return m.relayClient.Ready()
}

func (m *Manager) SetOnReconnectedListener(f func()) {
	m.listenerLock.Lock()
	defer m.listenerLock.Unlock()

	m.onReconnectedListenerFn = f
}

// AddCloseListener adds a listener to the given server instance address. The listener will be called if the connection
// closed.
func (m *Manager) AddCloseListener(serverAddress string, onClosedListener OnServerCloseListener) error {
	m.relayClientMu.RLock()
	defer m.relayClientMu.RUnlock()

	if m.relayClient == nil {
		return ErrRelayClientNotConnected
	}

	foreign, err := m.isForeignServer(serverAddress)
	if err != nil {
		return err
	}

	var listenerAddr string
	if foreign {
		listenerAddr = serverAddress
	} else {
		listenerAddr = m.relayClient.connectionURL
	}
	m.addListener(listenerAddr, onClosedListener)
	return nil
}

// RelayInstanceAddress returns the address and resolved IP of the permanent relay server. It could change if the
// network connection is lost. The address is sent to the target peer to choose the common relay server for the
// communication; the IP is sent alongside so remote peers can dial directly without their own DNS lookup. Both
// values are read under the same lock so they cannot diverge across a reconnection.
func (m *Manager) RelayInstanceAddress() (string, netip.Addr, error) {
	m.relayClientMu.RLock()
	defer m.relayClientMu.RUnlock()

	if m.relayClient == nil {
		return "", netip.Addr{}, ErrRelayClientNotConnected
	}
	addr, err := m.relayClient.ServerInstanceURL()
	if err != nil {
		return "", netip.Addr{}, err
	}
	return addr, m.relayClient.ConnectedIP(), nil
}

// ServerURLs returns the addresses of the relay servers.
func (m *Manager) ServerURLs() []string {
	return slices.Clone(m.serverPicker.ServerURLs.Load().([]string))
}

// HasRelayAddress returns true if the manager is serving. With this method can check if the peer can communicate with
// Relay service.
func (m *Manager) HasRelayAddress() bool {
	return len(m.serverPicker.ServerURLs.Load().([]string)) > 0
}

func (m *Manager) UpdateServerURLs(serverURLs []string) {
	m.UpdateServerURLsWithWeights(serverURLs, nil, nil)
}

func (m *Manager) UpdateServerURLsWithWeights(serverURLs []string, relayWeights map[string]int, preferredRelays map[string]struct{}) {
	log.Infof("update relay server URLs: %v", serverURLs)
	m.relayConfigMu.Lock()
	m.configuredRelayURLs = slices.Clone(serverURLs)
	m.relayWeights = relayWeightsFromURLs(serverURLs)
	m.preferredRelays = legacyPreferredRelays(serverURLs)
	for relayURL, weight := range relayWeights {
		if relayURL == "" || weight <= 0 {
			continue
		}
		m.relayWeights[relayURL] = weight
	}
	if preferredRelays != nil {
		m.preferredRelays = maps.Clone(preferredRelays)
	}
	if m.forcedRelayURL != "" && !slices.Contains(m.configuredRelayURLs, m.forcedRelayURL) {
		log.Warnf("forced Relay server %s is no longer in the received Relay list, clearing override", m.forcedRelayURL)
		m.forcedRelayURL = ""
	}
	effectiveURLs := m.effectiveRelayURLsLocked()
	m.relayConfigMu.Unlock()

	m.serverPicker.ServerURLs.Store(effectiveURLs)
	go m.switchHomeRelayIfNeeded(effectiveURLs)
	m.schedulePreferredHomeRelayReconcile()
}

// UpdateToken updates the token in the token store.
func (m *Manager) UpdateToken(token *relayAuth.Token) error {
	return m.tokenStore.UpdateToken(token)
}

func (m *Manager) RelayServers() []RelayServerInfo {
	m.relayConfigMu.RLock()
	configuredURLs := slices.Clone(m.configuredRelayURLs)
	relayWeights := maps.Clone(m.relayWeights)
	preferredRelays := maps.Clone(m.preferredRelays)
	forcedURL := m.forcedRelayURL
	effectiveURLs := m.effectiveRelayURLsLocked()
	m.relayConfigMu.RUnlock()

	currentURL := m.currentRelayURL()
	preferredURL := ""
	if len(effectiveURLs) > 0 {
		preferredURL = effectiveURLs[0]
	}

	result := make([]RelayServerInfo, 0, len(configuredURLs))
	for _, relayURL := range configuredURLs {
		weight := relayWeights[relayURL]
		if weight <= 0 {
			weight = defaultRelayWeight
		}
		if forcedURL != "" && relayURL != forcedURL && weight == preferredRelayWeight && containsRelay(preferredRelays, relayURL) {
			weight = defaultRelayWeight
		}
		if relayURL == forcedURL && weight < preferredRelayWeight {
			weight = preferredRelayWeight
		}
		result = append(result, RelayServerInfo{
			URL:       relayURL,
			Weight:    weight,
			Preferred: relayURL == preferredURL && containsRelay(preferredRelays, relayURL),
			Forced:    relayURL == forcedURL,
			Current:   relayURL == currentURL,
		})
	}
	return result
}

func (m *Manager) ProbeRelayServers(ctx context.Context) []RelayServerInfo {
	relays := m.RelayServers()

	var wg sync.WaitGroup
	for i := range relays {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			err := m.probeRelayServer(ctx, relays[idx].URL)
			if err != nil {
				relays[idx].Error = err.Error()
				return
			}
			relays[idx].Available = true
		}(i)
	}
	wg.Wait()

	return relays
}

func (m *Manager) probeRelayServer(ctx context.Context, relayURL string) error {
	probeCtx, cancel := context.WithTimeout(ctx, relayProbeTimeout)
	defer cancel()

	probeClient := NewClient(relayURL, m.tokenStore, m.peerID, m.mtu)
	if err := probeClient.Connect(probeCtx); err != nil {
		return err
	}
	if err := probeClient.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

func (m *Manager) SetForcedRelay(identifier string) (string, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return "", fmt.Errorf("relay identifier is required")
	}

	m.relayConfigMu.Lock()
	defer m.relayConfigMu.Unlock()

	if strings.EqualFold(identifier, "auto") || strings.EqualFold(identifier, "default") || strings.EqualFold(identifier, "clear") {
		m.forcedRelayURL = ""
		effectiveURLs := m.effectiveRelayURLsLocked()
		m.serverPicker.ServerURLs.Store(effectiveURLs)
		go m.switchHomeRelayIfNeeded(effectiveURLs)
		return "", nil
	}

	relayURL, err := matchRelayURL(identifier, m.configuredRelayURLs)
	if err != nil {
		return "", err
	}

	m.forcedRelayURL = relayURL
	effectiveURLs := m.effectiveRelayURLsLocked()
	m.serverPicker.ServerURLs.Store(effectiveURLs)
	go m.switchHomeRelayIfNeeded(effectiveURLs)
	return relayURL, nil
}

func (m *Manager) effectiveRelayURLsLocked() []string {
	if m.forcedRelayURL == "" {
		return slices.Clone(m.configuredRelayURLs)
	}

	result := make([]string, 0, len(m.configuredRelayURLs))
	result = append(result, m.forcedRelayURL)
	for _, relayURL := range m.configuredRelayURLs {
		if relayURL == m.forcedRelayURL {
			continue
		}
		result = append(result, relayURL)
	}
	return result
}

func relayWeightsFromURLs(relayURLs []string) map[string]int {
	weights := make(map[string]int, len(relayURLs))
	for idx, relayURL := range relayURLs {
		if relayURL == "" {
			continue
		}
		weight := defaultRelayWeight
		if idx == 0 {
			weight = preferredRelayWeight
		}
		weights[relayURL] = weight
	}
	return weights
}

func legacyPreferredRelays(relayURLs []string) map[string]struct{} {
	if len(relayURLs) == 0 || relayURLs[0] == "" {
		return nil
	}
	return map[string]struct{}{relayURLs[0]: {}}
}

func containsRelay(relays map[string]struct{}, relayURL string) bool {
	_, ok := relays[relayURL]
	return ok
}

func (m *Manager) currentRelayURL() string {
	m.relayClientMu.RLock()
	defer m.relayClientMu.RUnlock()
	if m.relayClient == nil {
		return ""
	}
	return m.relayClient.connectionURL
}

func matchRelayURL(identifier string, relayURLs []string) (string, error) {
	var matches []string
	normalizedIdentifier := strings.ToLower(identifier)
	for _, relayURL := range relayURLs {
		if relayURL == identifier {
			return relayURL, nil
		}
		parsedURL, err := url.Parse(relayURL)
		host := ""
		if err == nil {
			host = parsedURL.Hostname()
		}
		if strings.EqualFold(host, identifier) {
			return relayURL, nil
		}
		normalizedURL := strings.ToLower(relayURL)
		normalizedHost := strings.ToLower(host)
		if strings.Contains(normalizedURL, normalizedIdentifier) || strings.Contains(normalizedHost, normalizedIdentifier) {
			matches = append(matches, relayURL)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("relay %q was not found in received relay list", identifier)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("relay %q matches multiple relays: %s", identifier, strings.Join(matches, ", "))
	}
}

func (m *Manager) switchHomeRelayIfNeeded(serverURLs []string) {
	if len(serverURLs) == 0 {
		return
	}

	m.switchMu.Lock()
	defer m.switchMu.Unlock()

	m.relayClientMu.Lock()
	if !m.running || m.relayClient == nil || m.relayClient.connectionURL == serverURLs[0] {
		m.relayClientMu.Unlock()
		return
	}

	oldClient := m.relayClient
	oldURL := oldClient.connectionURL
	oldClient.SetOnDisconnectListener(nil)
	m.relayClient = nil
	m.relayClientMu.Unlock()

	log.Infof("preferred Relay server changed from %s to %s, switching home Relay server", oldURL, serverURLs[0])
	if err := oldClient.Close(); err != nil {
		log.Warnf("failed to close previous home Relay server %s: %v", oldURL, err)
	}

	newClient, err := m.serverPicker.PickServer(m.ctx)
	if err != nil {
		log.Errorf("failed to switch home Relay server: %s", err)
		go m.reconnectGuard.StartReconnectTrys(m.ctx, nil)
		return
	}

	m.storeClient(newClient)
	m.onServerConnected()
	m.schedulePreferredHomeRelayReconcile()
}

func (m *Manager) schedulePreferredHomeRelayReconcile() {
	go func() {
		timer := time.NewTimer(preferredReconcileDelay)
		defer timer.Stop()

		select {
		case <-timer.C:
			m.reconcilePreferredHomeRelay()
		case <-m.ctx.Done():
		}
	}()
}

func (m *Manager) reconcilePreferredHomeRelay() {
	m.relayConfigMu.RLock()
	effectiveURLs := m.effectiveRelayURLsLocked()
	m.relayConfigMu.RUnlock()
	if len(effectiveURLs) == 0 {
		return
	}

	preferredURL := effectiveURLs[0]
	if m.currentRelayURL() == preferredURL {
		return
	}

	log.Infof("current home Relay server is not preferred, probing preferred Relay server: %s", preferredURL)
	probeCtx, cancel := context.WithTimeout(m.ctx, preferredProbeTimeout)
	defer cancel()

	preferredClient := NewClient(preferredURL, m.tokenStore, m.peerID, m.mtu)
	if err := preferredClient.Connect(probeCtx); err != nil {
		log.Warnf("preferred Relay server probe failed for %s: %v", preferredURL, err)
		return
	}

	if !m.replaceHomeRelayWithPreferred(preferredClient, preferredURL) {
		if err := preferredClient.Close(); err != nil {
			log.Warnf("failed to close unused preferred Relay server %s: %v", preferredURL, err)
		}
	}
}

func (m *Manager) replaceHomeRelayWithPreferred(preferredClient *Client, preferredURL string) bool {
	m.switchMu.Lock()
	defer m.switchMu.Unlock()

	m.relayConfigMu.RLock()
	effectiveURLs := m.effectiveRelayURLsLocked()
	m.relayConfigMu.RUnlock()
	if len(effectiveURLs) == 0 || effectiveURLs[0] != preferredURL {
		return false
	}

	m.relayClientMu.Lock()
	if !m.running || m.relayClient == nil || m.relayClient.connectionURL == preferredURL {
		m.relayClientMu.Unlock()
		return false
	}

	oldClient := m.relayClient
	oldURL := oldClient.connectionURL
	oldClient.SetOnDisconnectListener(nil)
	m.relayClient = preferredClient
	m.relayClient.SetOnDisconnectListener(m.onServerDisconnected)
	m.relayClientMu.Unlock()

	log.Infof("switching home Relay server from %s to preferred Relay server %s", oldURL, preferredURL)
	if err := oldClient.Close(); err != nil {
		log.Warnf("failed to close previous home Relay server %s: %v", oldURL, err)
	}
	m.onServerConnected()
	return true
}

func (m *Manager) openConnVia(ctx context.Context, serverAddress, peerKey string, serverIP netip.Addr) (net.Conn, error) {
	// check if already has a connection to the desired relay server
	m.relayClientsMutex.RLock()
	rt, ok := m.relayClients[serverAddress]
	if ok {
		rt.RLock()
		m.relayClientsMutex.RUnlock()
		defer rt.RUnlock()
		if rt.err != nil {
			return nil, rt.err
		}
		return rt.relayClient.OpenConn(ctx, peerKey)
	}
	m.relayClientsMutex.RUnlock()

	// if not, establish a new connection but check it again (because changed the lock type) before starting the
	// connection
	m.relayClientsMutex.Lock()
	rt, ok = m.relayClients[serverAddress]
	if ok {
		rt.RLock()
		m.relayClientsMutex.Unlock()
		defer rt.RUnlock()
		if rt.err != nil {
			return nil, rt.err
		}
		return rt.relayClient.OpenConn(ctx, peerKey)
	}

	// create a new relay client and store it in the relayClients map
	rt = NewRelayTrack()
	rt.Lock()
	m.relayClients[serverAddress] = rt
	m.relayClientsMutex.Unlock()

	relayClient := NewClientWithServerIP(serverAddress, serverIP, m.tokenStore, m.peerID, m.mtu)
	err := relayClient.Connect(m.ctx)
	if err != nil {
		rt.err = err
		rt.Unlock()
		m.relayClientsMutex.Lock()
		delete(m.relayClients, serverAddress)
		m.relayClientsMutex.Unlock()
		return nil, err
	}
	// if connection closed then delete the relay client from the list
	relayClient.SetOnDisconnectListener(m.onServerDisconnected)
	rt.relayClient = relayClient
	rt.Unlock()

	conn, err := relayClient.OpenConn(ctx, peerKey)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (m *Manager) onServerConnected() {
	m.listenerLock.Lock()
	defer m.listenerLock.Unlock()

	if m.onReconnectedListenerFn == nil {
		return
	}
	go m.onReconnectedListenerFn()
}

// onServerDisconnected handles relay disconnect events. For the home server it
// starts the reconnect guard. For foreign servers it evicts the now-dead client
// from the cache so the next OpenConn builds a fresh one instead of reusing a
// closed client.
func (m *Manager) onServerDisconnected(serverAddress string) {
	m.relayClientMu.Lock()
	isHome := m.relayClient != nil && serverAddress == m.relayClient.connectionURL
	if isHome {
		go func(client *Client) {
			m.reconnectGuard.StartReconnectTrys(m.ctx, client)
		}(m.relayClient)
	}
	m.relayClientMu.Unlock()

	if !isHome {
		m.evictForeignRelay(serverAddress)
	}

	m.notifyOnDisconnectListeners(serverAddress)
}

func (m *Manager) evictForeignRelay(serverAddress string) {
	m.relayClientsMutex.Lock()
	defer m.relayClientsMutex.Unlock()
	if _, ok := m.relayClients[serverAddress]; ok {
		delete(m.relayClients, serverAddress)
		log.Debugf("evicted disconnected foreign relay client: %s", serverAddress)
	}
}

func (m *Manager) listenGuardEvent(ctx context.Context) {
	for {
		select {
		case <-m.reconnectGuard.OnReconnected:
			m.onServerConnected()
			m.schedulePreferredHomeRelayReconcile()
		case rc := <-m.reconnectGuard.OnNewRelayClient:
			m.storeClient(rc)
			m.onServerConnected()
			m.schedulePreferredHomeRelayReconcile()
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) storeClient(client *Client) {
	m.relayClientMu.Lock()
	defer m.relayClientMu.Unlock()

	m.relayClient = client
	m.relayClient.SetOnDisconnectListener(m.onServerDisconnected)
}

func (m *Manager) isForeignServer(address string) (bool, error) {
	rAddr, err := m.relayClient.ServerInstanceURL()
	if err != nil {
		return false, fmt.Errorf("relay client not connected")
	}
	return rAddr != address, nil
}

func (m *Manager) startCleanupLoop() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cleanUpUnusedRelays()
		}
	}
}

func (m *Manager) cleanUpUnusedRelays() {
	m.relayClientsMutex.Lock()
	defer m.relayClientsMutex.Unlock()

	for addr, rt := range m.relayClients {
		rt.Lock()
		// if the connection failed to the server the relay client will be nil
		// but the instance will be kept in the relayClients until the next locking
		if rt.err != nil {
			rt.Unlock()
			continue
		}

		if time.Since(rt.created) <= m.keepUnusedServerTime {
			rt.Unlock()
			continue
		}

		if rt.relayClient.HasConns() {
			rt.Unlock()
			continue
		}
		rt.relayClient.SetOnDisconnectListener(nil)
		go func() {
			_ = rt.relayClient.Close()
		}()
		log.Debugf("clean up unused relay server connection: %s", addr)
		delete(m.relayClients, addr)
		rt.Unlock()
	}
}

func (m *Manager) addListener(serverAddress string, onClosedListener OnServerCloseListener) {
	m.listenerLock.Lock()
	defer m.listenerLock.Unlock()
	l, ok := m.onDisconnectedListeners[serverAddress]
	if !ok {
		l = list.New()
	}
	for e := l.Front(); e != nil; e = e.Next() {
		if reflect.ValueOf(e.Value).Pointer() == reflect.ValueOf(onClosedListener).Pointer() {
			return
		}
	}
	l.PushBack(onClosedListener)
	m.onDisconnectedListeners[serverAddress] = l
}

func (m *Manager) notifyOnDisconnectListeners(serverAddress string) {
	m.listenerLock.Lock()
	defer m.listenerLock.Unlock()

	l, ok := m.onDisconnectedListeners[serverAddress]
	if !ok {
		return
	}
	for e := l.Front(); e != nil; e = e.Next() {
		go e.Value.(OnServerCloseListener)()
	}
	delete(m.onDisconnectedListeners, serverAddress)
}
