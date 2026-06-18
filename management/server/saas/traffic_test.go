package saas

import (
	"context"
	"testing"
	"time"

	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestTrafficServiceUsageAndIdempotentRelayEvents(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	seedTrafficAccount(t, ctx, s, now, 100)

	service := TrafficService{Store: s}
	if err := service.RecordRelayTraffic(ctx, RelayTrafficEvent{
		AccountID:  "account-a",
		EventID:    "relay-event-a",
		Bytes:      40,
		Direction:  "both",
		RecordedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordRelayTraffic(ctx, RelayTrafficEvent{
		AccountID:  "account-a",
		EventID:    "relay-event-a",
		Bytes:      40,
		Direction:  "both",
		RecordedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	usage, err := service.Usage(ctx, "account-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if usage.HighSpeedUsedBytes != 40 || usage.HighSpeedRemainingBytes != 60 {
		t.Fatalf("unexpected usage after duplicate events: %+v", usage)
	}
	if usage.EffectiveRateLimitMbps != 50 || usage.HighSpeedRateLimitMbps != 50 || !usage.RelayOnlyAccounting || !usage.FairShareEnabled {
		t.Fatalf("unexpected high speed rate policy: %+v", usage)
	}

	if err := service.RecordRelayTraffic(ctx, RelayTrafficEvent{
		AccountID:  "account-a",
		EventID:    "relay-event-b",
		Bytes:      60,
		RecordedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	usage, err = service.Usage(ctx, "account-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if usage.TrafficTier != types.SaaSTrafficTierStandard || usage.HighSpeedRemainingBytes != 0 {
		t.Fatalf("expected standard tier after quota exhaustion: %+v", usage)
	}
	if usage.EffectiveRateLimitMbps != 10 {
		t.Fatalf("expected standard effective rate 10 Mbps, got %+v", usage)
	}

	if err := service.RecordRelayTraffic(ctx, RelayTrafficEvent{
		AccountID:  "account-a",
		EventID:    "relay-event-c",
		Bytes:      25,
		RecordedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.GetSaaSTrafficLedgerForPeriod(ctx, store.LockingStrengthNone, "account-a", now.Format("2006-01"))
	if err != nil {
		t.Fatal(err)
	}
	var standardBytes int64
	for _, entry := range entries {
		if entry.Tier == types.SaaSTrafficTierStandard {
			standardBytes += entry.Bytes
		}
	}
	if standardBytes != 25 {
		t.Fatalf("expected 25 standard bytes, got %d from %+v", standardBytes, entries)
	}
}

func TestTrafficServiceApplyTrafficPurchaseRestoresHighSpeedQuota(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	seedTrafficAccount(t, ctx, s, now, 100)

	service := TrafficService{Store: s}
	if err := service.RecordRelayTraffic(ctx, RelayTrafficEvent{
		AccountID:  "account-a",
		EventID:    "relay-event-a",
		Bytes:      100,
		RecordedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	usage, err := service.Usage(ctx, "account-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if usage.TrafficTier != types.SaaSTrafficTierStandard {
		t.Fatalf("expected standard tier before purchase: %+v", usage)
	}

	purchase, err := service.ApplyTrafficPurchase(ctx, TrafficPurchaseRequest{
		AccountID:             "account-a",
		PurchaseID:            "purchase-a",
		HighSpeedTrafficBytes: 50,
		CreatedBy:             "platform-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Status != types.SaaSTrafficPurchaseStatusApplied {
		t.Fatalf("unexpected purchase: %+v", purchase)
	}
	stored, err := s.GetSaaSTrafficPurchase(ctx, store.LockingStrengthNone, "purchase-a")
	if err != nil {
		t.Fatal(err)
	}
	if stored.HighSpeedTrafficBytes != 50 || stored.CreatedBy != "platform-admin" {
		t.Fatalf("unexpected stored purchase: %+v", stored)
	}

	usage, err = service.Usage(ctx, "account-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if usage.TrafficTier != types.SaaSTrafficTierHighSpeed || usage.HighSpeedRemainingBytes != 50 {
		t.Fatalf("expected high speed restored after purchase: %+v", usage)
	}
}

func seedTrafficAccount(t *testing.T, ctx context.Context, s store.Store, now time.Time, highSpeedBytes int64) {
	t.Helper()
	if err := s.SaveSaaSOrganization(ctx, &types.SaaSOrganization{
		AccountID:   "account-a",
		Slug:        "accounta",
		Domain:      "accounta.cloink.4w.ink",
		DisplayName: "Account A",
		Status:      types.SaaSOrganizationStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSSubscription(ctx, &types.SaaSSubscription{
		AccountID:             "account-a",
		Plan:                  "free",
		Status:                types.SaaSSubscriptionStatusTrialing,
		HighSpeedTrafficBytes: highSpeedBytes,
		CreatedAt:             now,
		UpdatedAt:             now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSBandwidthPolicy(ctx, &types.SaaSBandwidthPolicy{
		AccountID:              "account-a",
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
