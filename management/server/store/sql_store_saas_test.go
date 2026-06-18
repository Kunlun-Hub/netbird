package store

import (
	"context"
	"testing"
	"time"

	"github.com/netbirdio/netbird/management/server/types"
)

func TestSqlStoreSaaSModels(t *testing.T) {
	ctx := context.Background()
	store, cleanUp, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanUp()

	now := time.Now().UTC()
	org := &types.SaaSOrganization{
		AccountID:   "account-a",
		Slug:        "team1234",
		Domain:      "team1234.cloink.4w.ink",
		DisplayName: "Team A",
		Status:      types.SaaSOrganizationStatusActive,
		CreatedBy:   "user-a",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.SaveSaaSOrganization(ctx, org); err != nil {
		t.Fatal(err)
	}

	byAccount, err := store.GetSaaSOrganizationByAccountID(ctx, LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if byAccount.Domain != org.Domain {
		t.Fatalf("unexpected org domain: %s", byAccount.Domain)
	}

	byDomain, err := store.GetSaaSOrganizationByDomain(ctx, LockingStrengthNone, org.Domain)
	if err != nil {
		t.Fatal(err)
	}
	if byDomain.AccountID != org.AccountID {
		t.Fatalf("unexpected account id: %s", byDomain.AccountID)
	}

	subscription := &types.SaaSSubscription{
		AccountID:             org.AccountID,
		Plan:                  "free",
		Status:                types.SaaSSubscriptionStatusActive,
		UsersLimit:            3,
		PeersLimit:            10,
		HighSpeedTrafficBytes: 100 << 30,
		Features:              map[string]bool{"routes": true},
	}
	if err := store.SaveSaaSSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}
	storedSubscription, err := store.GetSaaSSubscription(ctx, LockingStrengthNone, org.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSubscription.UsersLimit != 3 || !storedSubscription.Features["routes"] {
		t.Fatalf("unexpected subscription: %+v", storedSubscription)
	}

	policy := &types.SaaSBandwidthPolicy{
		AccountID:              org.AccountID,
		HighSpeedRateLimitMbps: 50,
		StandardRateLimitMbps:  10,
		TotalRateLimitMbps:     50,
		RelayOnlyAccounting:    true,
	}
	if err := store.SaveSaaSBandwidthPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	storedPolicy, err := store.GetSaaSBandwidthPolicy(ctx, LockingStrengthNone, org.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if storedPolicy.StandardRateLimitMbps != 10 {
		t.Fatalf("unexpected standard limit: %d", storedPolicy.StandardRateLimitMbps)
	}

	admin := &types.SaaSPlatformAdmin{
		UserID:  "platform-admin",
		Role:    types.SaaSPlatformRoleAdmin,
		Enabled: true,
	}
	if err := store.SaveSaaSPlatformAdmin(ctx, admin); err != nil {
		t.Fatal(err)
	}
	storedAdmin, err := store.GetSaaSPlatformAdmin(ctx, LockingStrengthNone, admin.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if !storedAdmin.Enabled || storedAdmin.Role != types.SaaSPlatformRoleAdmin {
		t.Fatalf("unexpected platform admin: %+v", storedAdmin)
	}

	menu := &types.SaaSOrgMenuVisibility{AccountID: org.AccountID, MenuKey: "dns", Visible: false}
	if err := store.SaveSaaSOrgMenuVisibility(ctx, menu); err != nil {
		t.Fatal(err)
	}
	menus, err := store.GetSaaSOrgMenuVisibility(ctx, LockingStrengthNone, org.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(menus) != 1 || menus[0].MenuKey != "dns" {
		t.Fatalf("unexpected menus: %+v", menus)
	}

	entry := &types.SaaSTrafficLedger{
		ID:         "ledger-a",
		AccountID:  org.AccountID,
		PeriodKey:  "2026-06",
		Source:     types.SaaSTrafficSourceRelay,
		Direction:  "both",
		Bytes:      1024,
		Tier:       types.SaaSTrafficTierHighSpeed,
		EventID:    "relay-event-a",
		RecordedAt: now,
	}
	if err := store.CreateSaaSTrafficLedger(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSaaSTrafficLedger(ctx, entry); err != nil {
		t.Fatal(err)
	}
	ledger, err := store.GetSaaSTrafficLedgerForPeriod(ctx, LockingStrengthNone, org.AccountID, "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger) != 1 || ledger[0].Bytes != 1024 {
		t.Fatalf("unexpected ledger: %+v", ledger)
	}
}
