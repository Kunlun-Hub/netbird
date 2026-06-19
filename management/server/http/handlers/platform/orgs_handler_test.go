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
	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/activity"
	emailmanager "github.com/netbirdio/netbird/management/server/email"
	nbpeer "github.com/netbirdio/netbird/management/server/peer"
	saasmanager "github.com/netbirdio/netbird/management/server/saas"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/route"
)

type fakeSubscriptionEmailService struct {
	kinds []types.EmailTemplateKind
}

func (f *fakeSubscriptionEmailService) Notify(_ context.Context, _ string, kind types.EmailTemplateKind, _ emailmanager.TemplateData) error {
	f.kinds = append(f.kinds, kind)
	return nil
}

func TestListOrganizations(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	eventStore := &activity.InMemoryEventStore{}
	AddEndpoints(s, testPlatformSaaSConfig(), router, eventStore, nil)

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
	emailSvc := &fakeSubscriptionEmailService{}
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, emailSvc)

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
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

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
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

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

func TestUpdateSubscription(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	emailSvc := &fakeSubscriptionEmailService{}
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, emailSvc)

	trialEndsAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	body := []byte(`{
		"plan": "pro",
		"status": "past_due",
		"trial_ends_at": "2026-07-01T00:00:00Z",
		"expires_at": "2026-08-01T00:00:00Z"
	}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/platform/orgs/account-a/subscription", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Plan != "pro" || subscription.Status != types.SaaSSubscriptionStatusPastDue {
		t.Fatalf("unexpected subscription: %+v", subscription)
	}
	if subscription.TrialEndsAt == nil || !subscription.TrialEndsAt.Equal(trialEndsAt) {
		t.Fatalf("unexpected trial ends at: %+v", subscription.TrialEndsAt)
	}
	if subscription.ExpiresAt == nil || !subscription.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("unexpected expires at: %+v", subscription.ExpiresAt)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/platform/orgs/account-a", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp organization
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.SubscriptionStatus != types.SaaSSubscriptionStatusPastDue || resp.TrialEndsAt != trialEndsAt.Format(time.RFC3339) || resp.ExpiresAt != expiresAt.Format(time.RFC3339) {
		t.Fatalf("unexpected organization subscription fields: %+v", resp)
	}
	if len(emailSvc.kinds) != 1 || emailSvc.kinds[0] != types.EmailTemplateSubscriptionStatusChanged {
		t.Fatalf("expected subscription status notification, got %+v", emailSvc.kinds)
	}
}

func TestUpdateSubscriptionRejectsInvalidStatus(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

	body := []byte(`{"status":"broken"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/platform/orgs/account-a/subscription", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected unprocessable entity, got %d: %s", rec.Code, rec.Body.String())
	}
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Status != types.SaaSSubscriptionStatusTrialing {
		t.Fatalf("subscription should not change on invalid input: %+v", subscription)
	}
}

func TestRenewSubscription(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)
	expiredAt := time.Now().UTC().Add(-24 * time.Hour)
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	subscription.Status = types.SaaSSubscriptionStatusPastDue
	subscription.ExpiresAt = &expiredAt
	if err := s.SaveSaaSSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}

	router := mux.NewRouter()
	emailSvc := &fakeSubscriptionEmailService{}
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, emailSvc)

	body := []byte(`{"plan":"pro","period_days":30,"high_speed_gb":10,"operator_id":"platform-admin","reason":"paid renewal"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/platform/orgs/account-a/subscription/renew", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp saasmanager.RenewSubscriptionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Subscription.Status != types.SaaSSubscriptionStatusActive || resp.Subscription.Plan != "pro" {
		t.Fatalf("unexpected renewed subscription: %+v", resp.Subscription)
	}
	if resp.Subscription.ExpiresAt == nil || !resp.Subscription.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("expected future expiration, got %+v", resp.Subscription.ExpiresAt)
	}
	if resp.Subscription.HighSpeedTrafficBytes != 1073741824+(10*1024*1024*1024) {
		t.Fatalf("unexpected renewed traffic quota: %d", resp.Subscription.HighSpeedTrafficBytes)
	}
	if len(emailSvc.kinds) != 1 || emailSvc.kinds[0] != types.EmailTemplateSubscriptionStatusChanged {
		t.Fatalf("expected renewal notification, got %+v", emailSvc.kinds)
	}
}

func TestProcessSubscriptionLifecycle(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)
	expiredAt := time.Now().UTC().Add(-48 * time.Hour)
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	subscription.Status = types.SaaSSubscriptionStatusActive
	subscription.ExpiresAt = &expiredAt
	if err := s.SaveSaaSSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}

	router := mux.NewRouter()
	emailSvc := &fakeSubscriptionEmailService{}
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, emailSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/platform/orgs/subscriptions/lifecycle", bytes.NewReader([]byte(`{}`)))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp saasmanager.SubscriptionLifecycleResult
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Changes) != 1 || resp.Changes[0].Current != types.SaaSSubscriptionStatusPastDue {
		t.Fatalf("unexpected lifecycle response: %+v", resp)
	}
	subscription, err = s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Status != types.SaaSSubscriptionStatusPastDue {
		t.Fatalf("expected past_due subscription, got %+v", subscription)
	}
	if len(emailSvc.kinds) != 1 || emailSvc.kinds[0] != types.EmailTemplateSubscriptionStatusChanged {
		t.Fatalf("expected lifecycle notification, got %+v", emailSvc.kinds)
	}
}

func TestUpdateDomain(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	eventStore := &activity.InMemoryEventStore{}
	AddEndpoints(s, testPlatformSaaSConfig(), router, eventStore, nil)

	body := []byte(`{"domain":"NewTeam123.cloink.4w.ink"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/platform/orgs/account-a/domain", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	org, err := s.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if org.Domain != "newteam123.cloink.4w.ink" || org.Slug != "newteam123" {
		t.Fatalf("unexpected organization domain: %+v", org)
	}
	account, err := s.GetAccount(ctx, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if account.Domain != org.Domain || account.Settings.DNSDomain != org.Domain || account.DomainCategory != types.PrivateCategory || !account.IsDomainPrimaryAccount {
		t.Fatalf("account domain was not synchronized: %+v", account)
	}
	events, err := eventStore.Get(ctx, "account-a", 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Activity != activity.SaaSOrganizationDomainUpdated {
		t.Fatalf("unexpected domain audit events: %+v", events)
	}
}

func TestUpdateDomainRejectsDuplicate(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)
	if err := s.SaveSaaSOrganization(ctx, &types.SaaSOrganization{
		AccountID:   "account-b",
		Slug:        "taken123",
		Domain:      "taken123.cloink.4w.ink",
		DisplayName: "Taken",
		Status:      types.SaaSOrganizationStatusActive,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	router := mux.NewRouter()
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/platform/orgs/account-a/domain", bytes.NewReader([]byte(`{"domain":"taken123.cloink.4w.ink"}`)))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected conflict, got %d: %s", rec.Code, rec.Body.String())
	}
	org, err := s.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if org.Domain != "team1234.cloink.4w.ink" {
		t.Fatalf("domain should not change on duplicate input: %+v", org)
	}
}

func TestUpdateMenus(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

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
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

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
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

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
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

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

func TestListBills(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)
	now := time.Now().UTC()
	bill := &types.SaaSBill{
		ID:             "bill-a",
		AccountID:      "account-a",
		PeriodKey:      "2026-06",
		Status:         types.SaaSBillStatusOpen,
		SubtotalCents:  9900,
		TotalCents:     9900,
		Currency:       "CNY",
		PaymentOrderID: "order-a",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.SaveSaaSBill(ctx, bill); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSBillItem(ctx, &types.SaaSBillItem{
		ID:                    "bill-item-a",
		BillID:                bill.ID,
		AccountID:             bill.AccountID,
		ItemType:              types.SaaSBillItemTypeTrafficPackage,
		Description:           "traffic package",
		Quantity:              1,
		UnitAmountCents:       9900,
		AmountCents:           9900,
		Currency:              "CNY",
		HighSpeedTrafficBytes: 100 << 30,
		CreatedAt:             now,
	}); err != nil {
		t.Fatal(err)
	}

	router := mux.NewRouter()
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/platform/orgs/account-a/bills", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp []saasmanager.BillResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) != 1 || resp[0].Bill.ID != bill.ID || len(resp[0].Items) != 1 || resp[0].Items[0].ID != "bill-item-a" {
		t.Fatalf("unexpected bills: %+v", resp)
	}
}

func TestRefundAndListRefunds(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)
	orderID := seedPaidTrafficOrder(t, ctx, s, 200, 9900)

	router := mux.NewRouter()
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

	body := []byte(`{"order_id":"` + orderID + `","amount_cents":9900,"reason":"customer request","operator_id":"platform-admin"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/platform/orgs/account-a/refunds", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var refundResp saasmanager.RefundResponse
	if err := json.NewDecoder(rec.Body).Decode(&refundResp); err != nil {
		t.Fatal(err)
	}
	if refundResp.Refund.PaymentOrderID != orderID || refundResp.Order.Status != types.SaaSPaymentOrderStatusRefunded {
		t.Fatalf("unexpected refund response: %+v", refundResp)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/platform/orgs/account-a/refunds", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var refunds []*types.SaaSPaymentRefund
	if err := json.NewDecoder(rec.Body).Decode(&refunds); err != nil {
		t.Fatal(err)
	}
	if len(refunds) != 1 || refunds[0].PaymentOrderID != orderID || refunds[0].AmountCents != 9900 {
		t.Fatalf("unexpected refunds: %+v", refunds)
	}
}

func TestCreateAndListReconciliationRecords(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)
	orderID := seedPaidTrafficOrder(t, ctx, s, 200, 9900)

	router := mux.NewRouter()
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

	body := []byte(`{"order_id":"` + orderID + `","actual_amount_cents":8800,"status":"matched","operator_id":"platform-admin"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/platform/orgs/account-a/reconciliation-records", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var reconciliationResp saasmanager.ReconciliationResponse
	if err := json.NewDecoder(rec.Body).Decode(&reconciliationResp); err != nil {
		t.Fatal(err)
	}
	if reconciliationResp.Record.Status != types.SaaSReconciliationStatusMismatch || reconciliationResp.Record.ExpectedAmountCents != 9900 {
		t.Fatalf("unexpected reconciliation response: %+v", reconciliationResp)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/platform/orgs/account-a/reconciliation-records", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var records []*types.SaaSReconciliationRecord
	if err := json.NewDecoder(rec.Body).Decode(&records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].PaymentOrderID != orderID || records[0].Status != types.SaaSReconciliationStatusMismatch {
		t.Fatalf("unexpected reconciliation records: %+v", records)
	}
}

func TestClosePaymentOrder(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)
	orderID := seedPendingTrafficOrder(t, ctx, s, 200, 9900)

	router := mux.NewRouter()
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

	body := []byte(`{"reason":"timeout","operator_id":"platform-admin"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/platform/orgs/account-a/payment-orders/"+orderID+"/close", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp saasmanager.ClosePaymentOrderResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Order.Status != types.SaaSPaymentOrderStatusClosed || resp.Purchase.Status != types.SaaSTrafficPurchaseStatusRefunded || resp.Bill.Status != types.SaaSBillStatusVoid {
		t.Fatalf("unexpected close response: %+v", resp)
	}
}

func TestInvoiceOfflinePaymentAndAutoRenewalEndpoints(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newPlatformTestStore(t, ctx)
	defer cleanup()
	seedPlatformOrganization(t, ctx, s)

	router := mux.NewRouter()
	AddEndpoints(s, testPlatformSaaSConfig(), router, &activity.InMemoryEventStore{}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/platform/orgs/account-a/invoices", bytes.NewReader([]byte(`{
		"invoice_title":"Cloink Ltd.",
		"tax_id":"tax-a",
		"email":"finance@example.com",
		"amount_cents":9900,
		"operator_id":"platform-admin"
	}`)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoice status = %d: %s", rec.Code, rec.Body.String())
	}
	var invoice types.SaaSInvoiceRequest
	if err := json.NewDecoder(rec.Body).Decode(&invoice); err != nil {
		t.Fatal(err)
	}
	if invoice.Status != types.SaaSInvoiceStatusRequested || invoice.AmountCents != 9900 {
		t.Fatalf("unexpected invoice: %+v", invoice)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/platform/orgs/account-a/invoices/"+invoice.ID, bytes.NewReader([]byte(`{
		"status":"issued",
		"invoice_no":"INV-20260619",
		"invoice_url":"https://example.com/invoice.pdf",
		"operator_id":"platform-admin"
	}`)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoice update status = %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&invoice); err != nil {
		t.Fatal(err)
	}
	if invoice.Status != types.SaaSInvoiceStatusIssued || invoice.InvoiceNo != "INV-20260619" {
		t.Fatalf("unexpected invoice update: %+v", invoice)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/platform/orgs/account-a/offline-payments", bytes.NewReader([]byte(`{
		"provider":"bank_transfer",
		"provider_trade_no":"bank-20260619",
		"amount_cents":19900,
		"status":"applied",
		"plan":"pro",
		"period_days":30,
		"high_speed_gb":10,
		"operator_id":"platform-admin"
	}`)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("offline payment status = %d: %s", rec.Code, rec.Body.String())
	}
	var offline types.SaaSOfflinePaymentRecord
	if err := json.NewDecoder(rec.Body).Decode(&offline); err != nil {
		t.Fatal(err)
	}
	if offline.Status != types.SaaSOfflinePaymentStatusApplied || offline.Provider != types.SaaSPaymentProviderBank {
		t.Fatalf("unexpected offline payment: %+v", offline)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/platform/orgs/account-a/auto-renewal-attempts", bytes.NewReader([]byte(`{
		"provider":"alipay",
		"amount_cents":19900,
		"status":"failed",
		"reason":"agreement missing"
	}`)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auto renewal status = %d: %s", rec.Code, rec.Body.String())
	}
	var attempt types.SaaSAutoRenewalAttempt
	if err := json.NewDecoder(rec.Body).Decode(&attempt); err != nil {
		t.Fatal(err)
	}
	if attempt.Status != types.SaaSAutoRenewalAttemptStatusFailed || attempt.Provider != types.SaaSPaymentProviderAlipay {
		t.Fatalf("unexpected auto renewal attempt: %+v", attempt)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/platform/orgs/account-a/invoices", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list invoices status = %d: %s", rec.Code, rec.Body.String())
	}
	var invoices []*types.SaaSInvoiceRequest
	if err := json.NewDecoder(rec.Body).Decode(&invoices); err != nil {
		t.Fatal(err)
	}
	if len(invoices) != 1 || invoices[0].ID != invoice.ID {
		t.Fatalf("unexpected invoices: %+v", invoices)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/platform/orgs/account-a/offline-payments", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list offline payments status = %d: %s", rec.Code, rec.Body.String())
	}
	var offlineRecords []*types.SaaSOfflinePaymentRecord
	if err := json.NewDecoder(rec.Body).Decode(&offlineRecords); err != nil {
		t.Fatal(err)
	}
	if len(offlineRecords) != 1 || offlineRecords[0].ID != offline.ID {
		t.Fatalf("unexpected offline payments: %+v", offlineRecords)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/platform/orgs/account-a/auto-renewal-attempts", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list auto renewal attempts status = %d: %s", rec.Code, rec.Body.String())
	}
	var attempts []*types.SaaSAutoRenewalAttempt
	if err := json.NewDecoder(rec.Body).Decode(&attempts); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].ID != attempt.ID {
		t.Fatalf("unexpected auto renewal attempts: %+v", attempts)
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

func testPlatformSaaSConfig() nbconfig.SaaSConfig {
	return nbconfig.SaaSConfig{
		Enabled:                     true,
		RootDomain:                  "cloink.4w.ink",
		SubscriptionGracePeriodDays: 7,
		SubscriptionCancelAfterDays: 30,
	}
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

func seedPendingTrafficOrder(t *testing.T, ctx context.Context, s store.Store, highSpeedTrafficBytes, amountCents int64) string {
	t.Helper()
	now := time.Now().UTC()
	purchaseID := "purchase-a"
	orderID := "order-a"
	if err := s.SaveSaaSTrafficPurchase(ctx, &types.SaaSTrafficPurchase{
		ID:                    purchaseID,
		AccountID:             "account-a",
		PackageType:           types.SaaSTrafficPurchasePackageOneTime,
		HighSpeedTrafficBytes: highSpeedTrafficBytes,
		AmountCents:           amountCents,
		Currency:              "CNY",
		PaymentProvider:       types.SaaSPaymentProviderAlipay,
		Status:                types.SaaSTrafficPurchaseStatusPending,
		CreatedBy:             "user-a",
		CreatedAt:             now,
		UpdatedAt:             now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSPaymentOrder(ctx, &types.SaaSPaymentOrder{
		ID:              orderID,
		AccountID:       "account-a",
		Provider:        types.SaaSPaymentProviderAlipay,
		ProviderTradeNo: "trade-a",
		Subject:         "traffic package",
		AmountCents:     amountCents,
		Currency:        "CNY",
		Status:          types.SaaSPaymentOrderStatusPending,
		PurchaseID:      purchaseID,
		CreatedBy:       "user-a",
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSBill(ctx, &types.SaaSBill{
		ID:              "bill-a",
		AccountID:       "account-a",
		PeriodKey:       now.Format("2006-01"),
		Status:          types.SaaSBillStatusOpen,
		SubtotalCents:   amountCents,
		TotalCents:      amountCents,
		Currency:        "CNY",
		PaymentProvider: types.SaaSPaymentProviderAlipay,
		PaymentOrderID:  orderID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}
	return orderID
}

func seedPaidTrafficOrder(t *testing.T, ctx context.Context, s store.Store, highSpeedTrafficBytes, amountCents int64) string {
	t.Helper()
	orderID := seedPendingTrafficOrder(t, ctx, s, highSpeedTrafficBytes, amountCents)
	now := time.Now().UTC()
	order, err := s.GetSaaSPaymentOrder(ctx, store.LockingStrengthNone, orderID)
	if err != nil {
		t.Fatal(err)
	}
	order.Status = types.SaaSPaymentOrderStatusPaid
	order.PaidAt = &now
	order.UpdatedAt = now
	if err := s.SaveSaaSPaymentOrder(ctx, order); err != nil {
		t.Fatal(err)
	}
	purchase, err := s.GetSaaSTrafficPurchase(ctx, store.LockingStrengthNone, order.PurchaseID)
	if err != nil {
		t.Fatal(err)
	}
	purchase.Status = types.SaaSTrafficPurchaseStatusApplied
	purchase.UpdatedAt = now
	if err := s.SaveSaaSTrafficPurchase(ctx, purchase); err != nil {
		t.Fatal(err)
	}
	bill, err := s.GetSaaSBillByPaymentOrderID(ctx, store.LockingStrengthNone, orderID)
	if err != nil {
		t.Fatal(err)
	}
	bill.Status = types.SaaSBillStatusPaid
	bill.PaidAt = &now
	bill.UpdatedAt = now
	if err := s.SaveSaaSBill(ctx, bill); err != nil {
		t.Fatal(err)
	}
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	subscription.HighSpeedTrafficBytes += highSpeedTrafficBytes
	subscription.UpdatedAt = now
	if err := s.SaveSaaSSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}
	return orderID
}
