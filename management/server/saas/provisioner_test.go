package saas

import (
	"context"
	"testing"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestProvisionOrganization(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	cfg := nbconfig.SaaSConfig{
		Enabled:                      true,
		PublicSignupEnabled:          true,
		OrganizationDomainSuffix:     "cloink.4w.ink",
		DefaultPlan:                  "free",
		DefaultUsersLimit:            3,
		DefaultPeersLimit:            10,
		DefaultRelaysLimit:           1,
		DefaultHighSpeedTrafficGB:    100,
		DefaultTotalRateLimitMbps:    50,
		DefaultStandardRateLimitMbps: 10,
		ForceRelayForTrafficBilling:  true,
	}
	result, err := (Provisioner{Store: s, Config: cfg}).ProvisionOrganization(ctx, "user-a", SignupRequest{
		Email:            "alice@example.com",
		Password:         "Password1!",
		Name:             "Alice",
		OrganizationName: "Alice Team",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID == "" || result.OrganizationDomain == "" || result.DashboardURL == "" {
		t.Fatalf("incomplete result: %+v", result)
	}

	account, err := s.GetAccount(ctx, result.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Domain != result.OrganizationDomain || account.Settings.DNSDomain != result.OrganizationDomain {
		t.Fatalf("organization domain was not synced to account: %+v", account)
	}
	owner, err := s.GetUserByUserID(ctx, store.LockingStrengthNone, "user-a")
	if err != nil {
		t.Fatal(err)
	}
	if owner.AccountID != result.AccountID || owner.Role != types.UserRoleOwner {
		t.Fatalf("unexpected owner: %+v", owner)
	}

	org, err := s.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthNone, result.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if org.Status != types.SaaSOrganizationStatusActive || org.DisplayName != "Alice Team" {
		t.Fatalf("unexpected SaaS org: %+v", org)
	}
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, result.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if subscription.UsersLimit != 3 || subscription.PeersLimit != 10 || subscription.HighSpeedTrafficBytes != 100<<30 {
		t.Fatalf("unexpected subscription: %+v", subscription)
	}
	policy, err := s.GetSaaSBandwidthPolicy(ctx, store.LockingStrengthNone, result.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.RelayOnlyAccounting || policy.TotalRateLimitMbps != 50 || policy.StandardRateLimitMbps != 10 {
		t.Fatalf("unexpected bandwidth policy: %+v", policy)
	}
	menus, err := s.GetSaaSOrgMenuVisibility(ctx, store.LockingStrengthNone, result.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(menus) != len(DefaultMenuKeys) {
		t.Fatalf("unexpected menus: %+v", menus)
	}
}

func TestProvisionOrganizationDisabled(t *testing.T) {
	_, err := (Provisioner{Config: nbconfig.SaaSConfig{Enabled: false}}).ProvisionOrganization(context.Background(), "user-a", SignupRequest{
		Email:            "alice@example.com",
		Password:         "Password1!",
		Name:             "Alice",
		OrganizationName: "Alice Team",
	})
	if err == nil {
		t.Fatal("expected disabled signup error")
	}
}
