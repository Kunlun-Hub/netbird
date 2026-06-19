package saas

import (
	"context"
	"testing"
	"time"

	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestSubscriptionStatusGuardRequireActive(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	guard := SubscriptionStatusGuard{Store: s, Now: func() time.Time { return time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC) }}
	if err := guard.RequireActive(ctx, "legacy-account"); err != nil {
		t.Fatalf("legacy account should be allowed: %v", err)
	}

	allowed := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
	expired := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name         string
		subscription *types.SaaSSubscription
		wantOK       bool
	}{
		{
			name: "active",
			subscription: &types.SaaSSubscription{
				AccountID:   "active-account",
				Plan:        "free",
				Status:      types.SaaSSubscriptionStatusActive,
				TrialEndsAt: &allowed,
				ExpiresAt:   &allowed,
			},
			wantOK: true,
		},
		{
			name: "trialing",
			subscription: &types.SaaSSubscription{
				AccountID:   "trial-account",
				Plan:        "trial",
				Status:      types.SaaSSubscriptionStatusTrialing,
				TrialEndsAt: &allowed,
			},
			wantOK: true,
		},
		{
			name: "expired",
			subscription: &types.SaaSSubscription{
				AccountID: "expired-account",
				Plan:      "free",
				Status:    types.SaaSSubscriptionStatusActive,
				ExpiresAt: &expired,
			},
			wantOK: false,
		},
		{
			name: "canceled",
			subscription: &types.SaaSSubscription{
				AccountID: "canceled-account",
				Plan:      "free",
				Status:    types.SaaSSubscriptionStatusCanceled,
			},
			wantOK: false,
		},
		{
			name: "past due",
			subscription: &types.SaaSSubscription{
				AccountID: "past-due-account",
				Plan:      "free",
				Status:    types.SaaSSubscriptionStatusPastDue,
			},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		if err := s.SaveSaaSSubscription(ctx, tc.subscription); err != nil {
			t.Fatal(err)
		}
		err := guard.RequireActive(ctx, tc.subscription.AccountID)
		if tc.wantOK && err != nil {
			t.Fatalf("%s should be allowed: %v", tc.name, err)
		}
		if !tc.wantOK && err == nil {
			t.Fatalf("%s should be blocked", tc.name)
		}
	}
}
