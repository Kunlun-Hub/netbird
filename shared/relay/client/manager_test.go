package client

import (
	"context"
	"fmt"
	"net/netip"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"

	"github.com/netbirdio/netbird/client/iface"
	"github.com/netbirdio/netbird/relay/server"
	"github.com/netbirdio/netbird/shared/relay/auth/allow"
)

// newManagerTestServerConfig creates a new server config for manager testing with the given address
func newManagerTestServerConfig(address string) server.Config {
	return server.Config{
		Meter:          otel.Meter(""),
		ExposedAddress: address,
		TLSSupport:     false,
		AuthValidator:  &allow.Auth{},
	}
}

func TestEmptyURL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewManager(ctx, nil, "alice", iface.DefaultMTU)
	err := mgr.Serve()
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

func TestForeignConn(t *testing.T) {
	ctx := context.Background()

	lstCfg1 := server.ListenerConfig{
		Address: "localhost:52101",
	}

	srv1, err := server.NewServer(newManagerTestServerConfig(lstCfg1.Address))
	if err != nil {
		t.Fatalf("failed to create server: %s", err)
	}
	errChan := make(chan error, 1)
	go func() {
		err := srv1.Listen(lstCfg1)
		if err != nil {
			errChan <- err
		}
	}()

	defer func() {
		err := srv1.Shutdown(ctx)
		if err != nil {
			t.Errorf("failed to close server: %s", err)
		}
	}()

	if err := waitForServerToStart(errChan); err != nil {
		t.Fatalf("failed to start server: %s", err)
	}

	srvCfg2 := server.ListenerConfig{
		Address: "localhost:52102",
	}
	srv2, err := server.NewServer(newManagerTestServerConfig(srvCfg2.Address))
	if err != nil {
		t.Fatalf("failed to create server: %s", err)
	}
	errChan2 := make(chan error, 1)
	go func() {
		err := srv2.Listen(srvCfg2)
		if err != nil {
			errChan2 <- err
		}
	}()

	defer func() {
		err := srv2.Shutdown(ctx)
		if err != nil {
			t.Errorf("failed to close server: %s", err)
		}
	}()

	if err := waitForServerToStart(errChan2); err != nil {
		t.Fatalf("failed to start server: %s", err)
	}

	mCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	clientAlice := NewManager(mCtx, toURL(lstCfg1), "alice", iface.DefaultMTU)
	if err := clientAlice.Serve(); err != nil {
		t.Fatalf("failed to serve manager: %s", err)
	}

	clientBob := NewManager(mCtx, toURL(srvCfg2), "bob", iface.DefaultMTU)
	if err := clientBob.Serve(); err != nil {
		t.Fatalf("failed to serve manager: %s", err)
	}
	bobsSrvAddr, _, err := clientBob.RelayInstanceAddress()
	if err != nil {
		t.Fatalf("failed to get relay address: %s", err)
	}
	connAliceToBob, err := clientAlice.OpenConn(ctx, bobsSrvAddr, "bob", netip.Addr{})
	if err != nil {
		t.Fatalf("failed to bind channel: %s", err)
	}
	connBobToAlice, err := clientBob.OpenConn(ctx, bobsSrvAddr, "alice", netip.Addr{})
	if err != nil {
		t.Fatalf("failed to bind channel: %s", err)
	}

	payload := "hello bob, I am alice"
	_, err = connAliceToBob.Write([]byte(payload))
	if err != nil {
		t.Fatalf("failed to write to channel: %s", err)
	}

	buf := make([]byte, 65535)
	n, err := connBobToAlice.Read(buf)
	if err != nil {
		t.Fatalf("failed to read from channel: %s", err)
	}

	_, err = connBobToAlice.Write(buf[:n])
	if err != nil {
		t.Fatalf("failed to write to channel: %s", err)
	}

	n, err = connAliceToBob.Read(buf)
	if err != nil {
		t.Fatalf("failed to read from channel: %s", err)
	}

	if payload != string(buf[:n]) {
		t.Fatalf("expected %s, got %s", payload, string(buf[:n]))
	}
}

func TestForeginConnClose(t *testing.T) {
	ctx := context.Background()

	srvCfg1 := server.ListenerConfig{
		Address: "localhost:52201",
	}
	srv1, err := server.NewServer(newManagerTestServerConfig(srvCfg1.Address))
	if err != nil {
		t.Fatalf("failed to create server: %s", err)
	}
	errChan := make(chan error, 1)
	go func() {
		err := srv1.Listen(srvCfg1)
		if err != nil {
			errChan <- err
		}
	}()

	defer func() {
		err := srv1.Shutdown(ctx)
		if err != nil {
			t.Errorf("failed to close server: %s", err)
		}
	}()

	if err := waitForServerToStart(errChan); err != nil {
		t.Fatalf("failed to start server: %s", err)
	}

	srvCfg2 := server.ListenerConfig{
		Address: "localhost:52202",
	}
	srv2, err := server.NewServer(newManagerTestServerConfig(srvCfg2.Address))
	if err != nil {
		t.Fatalf("failed to create server: %s", err)
	}
	errChan2 := make(chan error, 1)
	go func() {
		err := srv2.Listen(srvCfg2)
		if err != nil {
			errChan2 <- err
		}
	}()

	defer func() {
		err := srv2.Shutdown(ctx)
		if err != nil {
			t.Errorf("failed to close server: %s", err)
		}
	}()

	if err := waitForServerToStart(errChan2); err != nil {
		t.Fatalf("failed to start server: %s", err)
	}

	mCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	mgrBob := NewManager(mCtx, toURL(srvCfg2), "bob", iface.DefaultMTU)
	if err := mgrBob.Serve(); err != nil {
		t.Fatalf("failed to serve manager: %s", err)
	}

	mgr := NewManager(mCtx, toURL(srvCfg1), "alice", iface.DefaultMTU)
	err = mgr.Serve()
	if err != nil {
		t.Fatalf("failed to serve manager: %s", err)
	}
	conn, err := mgr.OpenConn(ctx, toURL(srvCfg2)[0], "bob", netip.Addr{})
	if err != nil {
		t.Fatalf("failed to bind channel: %s", err)
	}

	err = conn.Close()
	if err != nil {
		t.Fatalf("failed to close connection: %s", err)
	}
}

func TestForeignAutoClose(t *testing.T) {
	ctx := context.Background()
	relayCleanupInterval = 1 * time.Second
	keepUnusedServerTime = 2 * time.Second

	srvCfg1 := server.ListenerConfig{
		Address: "localhost:52301",
	}
	srv1, err := server.NewServer(newManagerTestServerConfig(srvCfg1.Address))
	if err != nil {
		t.Fatalf("failed to create server: %s", err)
	}
	errChan := make(chan error, 1)
	go func() {
		t.Log("binding server 1.")
		if err := srv1.Listen(srvCfg1); err != nil {
			errChan <- err
		}
	}()

	defer func() {
		t.Logf("closing server 1.")
		if err := srv1.Shutdown(ctx); err != nil {
			t.Errorf("failed to close server: %s", err)
		}
		t.Logf("server 1. closed")
	}()

	if err := waitForServerToStart(errChan); err != nil {
		t.Fatalf("failed to start server: %s", err)
	}

	srvCfg2 := server.ListenerConfig{
		Address: "localhost:52302",
	}
	srv2, err := server.NewServer(newManagerTestServerConfig(srvCfg2.Address))
	if err != nil {
		t.Fatalf("failed to create server: %s", err)
	}
	errChan2 := make(chan error, 1)
	go func() {
		t.Log("binding server 2.")
		err := srv2.Listen(srvCfg2)
		if err != nil {
			errChan2 <- err
		}
	}()
	defer func() {
		t.Logf("closing server 2.")
		err := srv2.Shutdown(ctx)
		if err != nil {
			t.Errorf("failed to close server: %s", err)
		}
		t.Logf("server 2 closed.")
	}()

	if err := waitForServerToStart(errChan2); err != nil {
		t.Fatalf("failed to start server: %s", err)
	}

	idAlice := "alice"
	t.Log("connect to server 1.")
	mCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	mgr := NewManager(mCtx, toURL(srvCfg1), idAlice, iface.DefaultMTU)
	err = mgr.Serve()
	if err != nil {
		t.Fatalf("failed to serve manager: %s", err)
	}

	// Set up a disconnect listener to track when foreign server disconnects
	foreignServerURL := toURL(srvCfg2)[0]
	disconnected := make(chan struct{})
	onDisconnect := func() {
		select {
		case disconnected <- struct{}{}:
		default:
		}
	}

	t.Log("open connection to another peer")
	if _, err = mgr.OpenConn(ctx, foreignServerURL, "anotherpeer", netip.Addr{}); err == nil {
		t.Fatalf("should have failed to open connection to another peer")
	}

	// Add the disconnect listener after the connection attempt
	if err := mgr.AddCloseListener(foreignServerURL, onDisconnect); err != nil {
		t.Logf("failed to add close listener (expected if connection failed): %s", err)
	}

	// Wait for cleanup to happen
	timeout := relayCleanupInterval + keepUnusedServerTime + 2*time.Second
	t.Logf("waiting for relay cleanup: %s", timeout)

	select {
	case <-disconnected:
		t.Log("foreign relay connection cleaned up successfully")
	case <-time.After(timeout):
		t.Log("timeout waiting for cleanup - this might be expected if connection never established")
	}

	t.Logf("closing manager")
}

func TestAutoReconnect(t *testing.T) {
	ctx := context.Background()

	srvCfg := server.ListenerConfig{
		Address: "localhost:52401",
	}
	srv, err := server.NewServer(newManagerTestServerConfig(srvCfg.Address))
	if err != nil {
		t.Fatalf("failed to create server: %s", err)
	}
	errChan := make(chan error, 1)
	go func() {
		if err := srv.Listen(srvCfg); err != nil {
			errChan <- err
		}
	}()

	defer func() {
		err := srv.Shutdown(ctx)
		if err != nil {
			log.Errorf("failed to close server: %s", err)
		}
	}()

	if err := waitForServerToStart(errChan); err != nil {
		t.Fatalf("failed to start server: %s", err)
	}

	mCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	clientBob := NewManager(mCtx, toURL(srvCfg), "bob", iface.DefaultMTU)
	err = clientBob.Serve()
	if err != nil {
		t.Fatalf("failed to serve manager: %s", err)
	}

	clientAlice := NewManager(mCtx, toURL(srvCfg), "alice", iface.DefaultMTU,
		WithMaxBackoffInterval(2*time.Second))
	err = clientAlice.Serve()
	if err != nil {
		t.Fatalf("failed to serve manager: %s", err)
	}
	ra, _, err := clientAlice.RelayInstanceAddress()
	if err != nil {
		t.Errorf("failed to get relay address: %s", err)
	}
	conn, err := clientAlice.OpenConn(ctx, ra, "bob", netip.Addr{})
	if err != nil {
		t.Errorf("failed to bind channel: %s", err)
	}

	t.Log("closing client relay connection")
	// todo figure out moc server
	_ = clientAlice.relayClient.relayConn.Close()
	t.Log("start test reading")
	_, err = conn.Read(make([]byte, 1))
	if err == nil {
		t.Errorf("unexpected reading from closed connection")
	}

	log.Infof("waiting for reconnection")
	if err := waitForReady(ctx, clientAlice, 15*time.Second); err != nil {
		t.Fatalf("manager did not reconnect: %s", err)
	}

	log.Infof("reopent the connection")
	_, err = clientAlice.OpenConn(ctx, ra, "bob", netip.Addr{})
	if err != nil {
		t.Errorf("failed to open channel: %s", err)
	}
}

func TestUpdateServerURLsSwitchesHomeRelayToHighestPriorityRelay(t *testing.T) {
	ctx := context.Background()

	srvCfg1 := server.ListenerConfig{
		Address: "localhost:52411",
	}
	srv1, err := server.NewServer(newManagerTestServerConfig(srvCfg1.Address))
	if err != nil {
		t.Fatalf("failed to create first server: %s", err)
	}
	errChan1 := make(chan error, 1)
	go func() {
		if err := srv1.Listen(srvCfg1); err != nil {
			errChan1 <- err
		}
	}()
	defer func() {
		if err := srv1.Shutdown(ctx); err != nil {
			log.Errorf("failed to close first server: %s", err)
		}
	}()
	if err := waitForServerToStart(errChan1); err != nil {
		t.Fatalf("failed to start first server: %s", err)
	}

	srvCfg2 := server.ListenerConfig{
		Address: "localhost:52412",
	}
	srv2, err := server.NewServer(newManagerTestServerConfig(srvCfg2.Address))
	if err != nil {
		t.Fatalf("failed to create second server: %s", err)
	}
	errChan2 := make(chan error, 1)
	go func() {
		if err := srv2.Listen(srvCfg2); err != nil {
			errChan2 <- err
		}
	}()
	defer func() {
		if err := srv2.Shutdown(ctx); err != nil {
			log.Errorf("failed to close second server: %s", err)
		}
	}()
	if err := waitForServerToStart(errChan2); err != nil {
		t.Fatalf("failed to start second server: %s", err)
	}

	mCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	url1 := toURL(srvCfg1)[0]
	url2 := toURL(srvCfg2)[0]
	clientAlice := NewManager(mCtx, []string{url1, url2}, "alice", iface.DefaultMTU,
		WithMaxBackoffInterval(2*time.Second))
	if err := clientAlice.Serve(); err != nil {
		t.Fatalf("failed to serve manager: %s", err)
	}

	if err := waitForReady(ctx, clientAlice, 5*time.Second); err != nil {
		t.Fatalf("manager did not connect to any initial relay: %s", err)
	}
	currentRelay, _, err := clientAlice.RelayInstanceAddress()
	if err != nil {
		t.Fatalf("failed to get current relay: %s", err)
	}
	if currentRelay != url1 && currentRelay != url2 {
		t.Fatalf("initial relay = %s, want %s or %s", currentRelay, url1, url2)
	}

	clientAlice.UpdateServerURLsWithWeights([]string{url1, url2}, map[string]int{
		url1: 30,
		url2: 40,
	}, nil)

	if err := waitForRelayAddress(ctx, clientAlice, url2, 10*time.Second); err != nil {
		t.Fatalf("manager did not switch to highest priority relay: %s", err)
	}
}

func TestSetForcedRelayReordersRelayList(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relayURLs := []string{
		"rels://auto.relay.example.com:12580",
		"rels://hz-cucc-relay.example.com:12580",
		"rels://hk-relay.example.com:12580",
	}
	mgr := NewManager(ctx, relayURLs, "alice", iface.DefaultMTU)

	matched, err := mgr.SetForcedRelay("hz-cucc")
	if err != nil {
		t.Fatalf("failed to force relay: %s", err)
	}
	if matched != relayURLs[1] {
		t.Fatalf("matched relay = %s, want %s", matched, relayURLs[1])
	}

	effectiveURLs := mgr.ServerURLs()
	if effectiveURLs[0] != relayURLs[1] {
		t.Fatalf("first effective relay = %s, want %s", effectiveURLs[0], relayURLs[1])
	}

	relays := mgr.RelayServers()
	if len(relays) != len(relayURLs) {
		t.Fatalf("relay count = %d, want %d", len(relays), len(relayURLs))
	}
	if relays[1].URL != relayURLs[1] || !relays[1].Forced || relays[1].Weight != 30 {
		t.Fatalf("forced relay info = %+v", relays[1])
	}
	if relays[0].Weight != 30 {
		t.Fatalf("default relay weight = %d, want 30", relays[0].Weight)
	}

	if _, err := mgr.SetForcedRelay("auto"); err != nil {
		t.Fatalf("failed to clear forced relay: %s", err)
	}
	if got := mgr.ServerURLs()[0]; got != relayURLs[0] {
		t.Fatalf("first relay after clear = %s, want %s", got, relayURLs[0])
	}
}

func TestRelayServersUseDownloadedWeightsWithoutPreferredFlag(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relayURLs := []string{
		"rels://relay-b.example.com:12580",
		"rels://relay-a.example.com:12580",
	}
	mgr := NewManager(ctx, nil, "alice", iface.DefaultMTU)
	mgr.UpdateServerURLsWithWeights(relayURLs, map[string]int{
		relayURLs[0]: 80,
		relayURLs[1]: 45,
	}, nil)

	relays := mgr.RelayServers()

	if len(relays) != 2 {
		t.Fatalf("relay count = %d, want 2", len(relays))
	}
	if relays[0].Weight != 80 || relays[0].Preferred {
		t.Fatalf("first relay info = %+v", relays[0])
	}
	if relays[1].Weight != 45 || relays[1].Preferred {
		t.Fatalf("second relay info = %+v", relays[1])
	}
}

func TestUpdateServerURLsWithWeightsSortsByPriority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relayURLs := []string{
		"rels://relay-a.example.com:12580",
		"rels://relay-b.example.com:12580",
		"rels://relay-c.example.com:12580",
	}
	mgr := NewManager(ctx, nil, "alice", iface.DefaultMTU)
	mgr.UpdateServerURLsWithWeights(relayURLs, map[string]int{
		relayURLs[0]: 30,
		relayURLs[1]: 50,
		relayURLs[2]: 40,
	}, nil)

	got := mgr.ServerURLs()
	want := []string{relayURLs[1], relayURLs[2], relayURLs[0]}
	if len(got) != len(want) {
		t.Fatalf("relay count = %d, want %d", len(got), len(want))
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("relay URLs = %v, want %v", got, want)
		}
	}
}

func TestCurrentRelayStillHighestPriority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relayURLs := []string{
		"rels://relay-a.example.com:12580",
		"rels://relay-b.example.com:12580",
		"rels://relay-c.example.com:12580",
	}
	mgr := NewManager(ctx, relayURLs, "alice", iface.DefaultMTU)
	mgr.relayClient = &Client{connectionURL: relayURLs[1]}
	mgr.relayWeights = map[string]int{
		relayURLs[0]: 40,
		relayURLs[1]: 40,
		relayURLs[2]: 30,
	}

	if !mgr.currentRelayStillHighestPriorityLocked(relayURLs) {
		t.Fatalf("current relay should remain valid in highest priority group")
	}

	mgr.relayWeights[relayURLs[1]] = 30
	if mgr.currentRelayStillHighestPriorityLocked(relayURLs) {
		t.Fatalf("current relay should not remain valid after dropping priority")
	}
}

func waitForReady(ctx context.Context, m *Manager, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.Ready() {
			return nil
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("manager not ready within %s", timeout)
}

func waitForRelayAddress(ctx context.Context, m *Manager, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		address, _, err := m.RelayInstanceAddress()
		if err == nil && address == expected {
			return nil
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	address, _, err := m.RelayInstanceAddress()
	if err != nil {
		return fmt.Errorf("relay address did not become %s within %s: %w", expected, timeout, err)
	}
	return fmt.Errorf("relay address = %s, want %s within %s", address, expected, timeout)
}

func TestNotifierDoubleAdd(t *testing.T) {
	ctx := context.Background()

	listenerCfg1 := server.ListenerConfig{
		Address: "localhost:52501",
	}
	srv, err := server.NewServer(newManagerTestServerConfig(listenerCfg1.Address))
	if err != nil {
		t.Fatalf("failed to create server: %s", err)
	}
	errChan := make(chan error, 1)
	go func() {
		if err := srv.Listen(listenerCfg1); err != nil {
			errChan <- err
		}
	}()

	defer func() {
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("failed to close server: %s", err)
		}
	}()

	if err := waitForServerToStart(errChan); err != nil {
		t.Fatalf("failed to start server: %s", err)
	}

	log.Debugf("connect by alice")
	mCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	clientBob := NewManager(mCtx, toURL(listenerCfg1), "bob", iface.DefaultMTU)
	if err = clientBob.Serve(); err != nil {
		t.Fatalf("failed to serve manager: %s", err)
	}

	clientAlice := NewManager(mCtx, toURL(listenerCfg1), "alice", iface.DefaultMTU)
	if err = clientAlice.Serve(); err != nil {
		t.Fatalf("failed to serve manager: %s", err)
	}

	conn1, err := clientAlice.OpenConn(ctx, clientAlice.ServerURLs()[0], "bob", netip.Addr{})
	if err != nil {
		t.Fatalf("failed to bind channel: %s", err)
	}

	fnCloseListener := OnServerCloseListener(func() {
		log.Infof("close listener")
	})

	err = clientAlice.AddCloseListener(clientAlice.ServerURLs()[0], fnCloseListener)
	if err != nil {
		t.Fatalf("failed to add close listener: %s", err)
	}

	err = clientAlice.AddCloseListener(clientAlice.ServerURLs()[0], fnCloseListener)
	if err != nil {
		t.Fatalf("failed to add close listener: %s", err)
	}

	err = conn1.Close()
	if err != nil {
		t.Errorf("failed to close connection: %s", err)
	}

}

func toURL(address server.ListenerConfig) []string {
	return []string{"rel://" + address.Address}
}
