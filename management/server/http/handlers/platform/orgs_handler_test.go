package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/gorilla/mux"

	nbdns "github.com/netbirdio/netbird/dns"
	"github.com/netbirdio/netbird/management/server/activity"
	nbpeer "github.com/netbirdio/netbird/management/server/peer"
	saasmanager "github.com/netbirdio/netbird/management/server/saas"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/route"
)

func TestListOrganizations(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	eventStore := &activity.InMemoryEventStore{}
	AddEndpoints(s, router, eventStore)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/platform/orgs", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp []organization
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) != 1 || resp[0].AccountID != "account-a" || resp[0].Domain != "team1234.cloink.4w.ink" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp[0].UserCount != 2 || resp[0].PeerCount != 1 || resp[0].UsersLimit != 3 || resp[0].PeersLimit != 10 {
		t.Fatalf("unexpected usage and limits: %+v", resp[0])
	}
}

func TestGetOrganization(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	AddEndpoints(s, router, &activity.InMemoryEventStore{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/platform/orgs/account-a", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp organization
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccountID != "account-a" || resp.OwnerEmail != "owner@example.com" {
		t.Fatalf("unexpected organization detail: %+v", resp)
	}
	if resp.UserCount != 2 || resp.PeerCount != 1 || resp.Plan != "free" || resp.TotalRateLimitMbps != 50 {
		t.Fatalf("unexpected usage or subscription detail: %+v", resp)
	}
}

func TestSuspendAndResumeOrganization(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	AddEndpoints(s, router, &activity.InMemoryEventStore{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/platform/orgs/account-a/suspend", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("suspend status = %d: %s", rec.Code, rec.Body.String())
	}
	org, err := s.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if org.Status != types.SaaSOrganizationStatusSuspended {
		t.Fatalf("expected suspended status, got %q", org.Status)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/platform/orgs/account-a/resume", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("resume status = %d: %s", rec.Code, rec.Body.String())
	}
	org, err = s.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if org.Status != types.SaaSOrganizationStatusActive {
		t.Fatalf("expected active status, got %q", org.Status)
	}
}

func TestUpdateLimits(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	AddEndpoints(s, router, &activity.InMemoryEventStore{})

	body := []byte(`{
		"users_limit": 7,
		"peers_limit": 20,
		"relays_limit": 3,
		"high_speed_traffic_bytes": 2147483648,
		"standard_rate_limit_mbps": 15,
		"total_rate_limit_mbps": 80
	}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/platform/orgs/account-a/limits", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.UsersLimit != 7 || subscription.PeersLimit != 20 || subscription.RelaysLimit != 3 {
		t.Fatalf("unexpected subscription limits: %+v", subscription)
	}
	if subscription.HighSpeedTrafficBytes != 2147483648 || subscription.StandardRateLimitMbps != 15 || subscription.TotalRateLimitMbps != 80 {
		t.Fatalf("unexpected subscription traffic limits: %+v", subscription)
	}
	policy, err := s.GetSaaSBandwidthPolicy(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if policy.StandardRateLimitMbps != 15 || policy.TotalRateLimitMbps != 80 || policy.HighSpeedRateLimitMbps != 80 {
		t.Fatalf("unexpected bandwidth policy: %+v", policy)
	}
}

func TestUpdateMenus(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	AddEndpoints(s, router, &activity.InMemoryEventStore{})

	body := []byte(`{"menus":{"routes":true,"dns":false}}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/platform/orgs/account-a/menus", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	menus, err := s.GetSaaSOrgMenuVisibility(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, menu := range menus {
		got[menu.MenuKey] = menu.Visible
	}
	if got["routes"] != true || got["dns"] != false {
		t.Fatalf("unexpected menus: %+v", got)
	}
}

func TestGetUsage(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	now := time.Now().UTC()
	if err := s.CreateSaaSTrafficLedger(ctx, &types.SaaSTrafficLedger{
		ID:         "ledger-a",
		AccountID:  "account-a",
		PeriodKey:  now.Format("2006-01"),
		Source:     types.SaaSTrafficSourceRelay,
		Direction:  "both",
		Bytes:      128,
		Tier:       types.SaaSTrafficTierHighSpeed,
		EventID:    "event-a",
		RecordedAt: now,
		CreatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSaaSTrafficLedger(ctx, &types.SaaSTrafficLedger{
		ID:         "ledger-b",
		AccountID:  "account-a",
		PeriodKey:  now.Format("2006-01"),
		Source:     types.SaaSTrafficSourceRelay,
		Direction:  "both",
		Bytes:      64,
		Tier:       types.SaaSTrafficTierStandard,
		EventID:    "event-b",
		RecordedAt: now,
		CreatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}

	router := mux.NewRouter()
	AddEndpoints(s, router, &activity.InMemoryEventStore{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/platform/orgs/account-a/usage", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp saasmanager.TrafficUsage
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.HighSpeedUsedBytes != 128 || resp.HighSpeedRemainingBytes != 1073741696 {
		t.Fatalf("unexpected usage: %+v", resp)
	}
	if resp.TrafficTier != types.SaaSTrafficTierHighSpeed || resp.StandardRateLimitMbps != 10 || resp.TotalRateLimitMbps != 50 {
		t.Fatalf("unexpected traffic policy detail: %+v", resp)
	}
}

func TestCreateTrafficPurchase(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	AddEndpoints(s, router, &activity.InMemoryEventStore{})

	body := []byte(`{"package_type":"one_time","high_speed_traffic_bytes":512,"created_by":"platform-admin"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/platform/orgs/account-a/traffic-purchases", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp types.SaaSTrafficPurchase
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccountID != "account-a" || resp.HighSpeedTrafficBytes != 512 || resp.Status != types.SaaSTrafficPurchaseStatusApplied {
		t.Fatalf("unexpected purchase response: %+v", resp)
	}
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.HighSpeedTrafficBytes != 1073741824+512 {
		t.Fatalf("unexpected high speed quota after purchase: %d", subscription.HighSpeedTrafficBytes)
	}
}

func TestListPaymentOrders(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)
	now := time.Now().UTC()
	if err := s.SaveSaaSPaymentOrder(ctx, &types.SaaSPaymentOrder{
		ID:          "order-a",
		AccountID:   "account-a",
		Provider:    types.SaaSPaymentProviderAlipay,
		Subject:     "traffic package",
		AmountCents: 9900,
		Currency:    "CNY",
		Status:      types.SaaSPaymentOrderStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}

	router := mux.NewRouter()
	AddEndpoints(s, router, &activity.InMemoryEventStore{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/platform/orgs/account-a/payment-orders", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp []*types.SaaSPaymentOrder
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) != 1 || resp[0].ID != "order-a" || resp[0].Provider != types.SaaSPaymentProviderAlipay {
		t.Fatalf("unexpected orders: %+v", resp)
	}
}

func newPlatformTestStore(t *testing.T, ctx context.Context) (store.Store, func()) {
	t.Helper()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s, cleanup
}

func seedPlatformOrganization(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()

	now := time.Now().UTC()
	account := &types.Account{
		Id:               "account-a",
		CreatedBy:        "owner-a",
		CreatedAt:        now,
		Domain:           "team1234.cloink.4w.ink",
		DomainCategory:   types.PrivateCategory,
		SetupKeys:        map[string]*types.SetupKey{},
		Network:          types.NewNetwork(),
		Peers:            map[string]*nbpeer.Peer{},
		Users:            map[string]*types.User{},
		Groups:           map[string]*types.Group{},
		Routes:           map[route.ID]*route.Route{},
		NameServerGroups: map[string]*nbdns.NameServerGroup{},
		Settings:         &types.Settings{},
	}
	owner := types.NewOwnerUser("owner-a", "owner@example.com", "Owner")
	owner.AccountID = account.Id
	owner.CreatedAt = now
	member := types.NewRegularUser("user-a", "user@example.com", "User")
	member.AccountID = account.Id
	member.CreatedAt = now
	peer := &nbpeer.Peer{
		ID:        "peer-a",
		AccountID: account.Id,
		Key:       "peer-key-a",
		IP:        netip.MustParseAddr("100.64.0.10"),
		Name:      "peer-a",
		DNSLabel:  "peer-a",
		UserID:    owner.Id,
		CreatedAt: now,
	}
	account.Users[owner.Id] = owner
	account.Users[member.Id] = member
	account.Peers[peer.ID] = peer

	if err := s.SaveAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSOrganization(ctx, &types.SaaSOrganization{
		AccountID:   account.Id,
		Slug:        "team1234",
		Domain:      "team1234.cloink.4w.ink",
		DisplayName: "Team A",
		Status:      types.SaaSOrganizationStatusActive,
		CreatedBy:   owner.Id,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSSubscription(ctx, &types.SaaSSubscription{
		AccountID:             account.Id,
		Plan:                  "free",
		Status:                types.SaaSSubscriptionStatusTrialing,
		UsersLimit:            3,
		PeersLimit:            10,
		RelaysLimit:           1,
		HighSpeedTrafficBytes: 1073741824,
		StandardRateLimitMbps: 10,
		TotalRateLimitMbps:    50,
		CreatedAt:             now,
		UpdatedAt:             now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSBandwidthPolicy(ctx, &types.SaaSBandwidthPolicy{
		AccountID:              account.Id,
		HighSpeedRateLimitMbps: 50,
		StandardRateLimitMbps:  10,
		TotalRateLimitMbps:     50,
		RelayOnlyAccounting:    true,
		FairShareEnabled:       true,
		UpdatedAt:              now,
	}); err != nil {
		t.Fatal(err)
	}
}
