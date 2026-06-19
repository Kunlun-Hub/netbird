package saas

import (
	"context"
	"testing"
	"time"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestSubscriptionLifecycleProcessTransitionsExpiredSubscriptions(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	seedTrafficAccount(t, ctx, s, now.Add(-40*24*time.Hour), 100)
	expiredAt := now.Add(-8 * 24 * time.Hour)
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	subscription.Status = types.SaaSSubscriptionStatusPastDue
	subscription.ExpiresAt = &expiredAt
	if err := s.SaveSaaSSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}

	result, err := (SubscriptionLifecycleService{
		Store: s,
		Config: nbconfig.SaaSConfig{
			SubscriptionGracePeriodDays: 7,
			SubscriptionCancelAfterDays: 30,
		},
		Now: func() time.Time { return now },
	}).Process(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 || result.Changes[0].Previous != types.SaaSSubscriptionStatusPastDue || result.Changes[0].Current != types.SaaSSubscriptionStatusSuspended {
		t.Fatalf("unexpected lifecycle changes: %+v", result.Changes)
	}
	subscription, err = s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Status != types.SaaSSubscriptionStatusSuspended {
		t.Fatalf("expected suspended subscription, got %+v", subscription)
	}

	expiredAt = now.Add(-31 * 24 * time.Hour)
	subscription.ExpiresAt = &expiredAt
	subscription.Status = types.SaaSSubscriptionStatusSuspended
	if err := s.SaveSaaSSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}
	result, err = (SubscriptionLifecycleService{
		Store: s,
		Config: nbconfig.SaaSConfig{
			SubscriptionGracePeriodDays: 7,
			SubscriptionCancelAfterDays: 30,
		},
		Now: func() time.Time { return now },
	}).Process(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 || result.Changes[0].Current != types.SaaSSubscriptionStatusCanceled {
		t.Fatalf("unexpected cancel transition: %+v", result.Changes)
	}
}

func TestSubscriptionLifecycleProcessMarksExpiredTrialPastDue(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	seedTrafficAccount(t, ctx, s, now.Add(-10*24*time.Hour), 100)
	trialEndsAt := now.Add(-1 * time.Hour)
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	subscription.Status = types.SaaSSubscriptionStatusTrialing
	subscription.TrialEndsAt = &trialEndsAt
	if err := s.SaveSaaSSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}

	result, err := (SubscriptionLifecycleService{Store: s, Now: func() time.Time { return now }}).Process(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 || result.Changes[0].Current != types.SaaSSubscriptionStatusPastDue {
		t.Fatalf("unexpected lifecycle changes: %+v", result.Changes)
	}
}

func TestSubscriptionLifecycleRenewRestoresPastDueSubscription(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	seedTrafficAccount(t, ctx, s, now.Add(-10*24*time.Hour), 100)
	expiredAt := now.Add(-24 * time.Hour)
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	subscription.Status = types.SaaSSubscriptionStatusPastDue
	subscription.ExpiresAt = &expiredAt
	if err := s.SaveSaaSSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}

	resp, err := (SubscriptionLifecycleService{Store: s, Now: func() time.Time { return now }}).Renew(ctx, RenewSubscriptionRequest{
		AccountID:   "account-a",
		Plan:        "pro",
		PeriodDays:  30,
		HighSpeedGB: 5,
		OperatorID:  "platform-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Subscription.Status != types.SaaSSubscriptionStatusActive || resp.Subscription.Plan != "pro" {
		t.Fatalf("unexpected renewed subscription: %+v", resp.Subscription)
	}
	if resp.Subscription.ExpiresAt == nil || !resp.Subscription.ExpiresAt.Equal(now.AddDate(0, 0, 30)) {
		t.Fatalf("unexpected expiration: %+v", resp.Subscription.ExpiresAt)
	}
	if resp.Subscription.HighSpeedTrafficBytes != 100+(5*1024*1024*1024) {
		t.Fatalf("unexpected quota after renewal: %d", resp.Subscription.HighSpeedTrafficBytes)
	}
}
