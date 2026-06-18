package saas

import (
	"context"
	"testing"
	"time"

	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestOrganizationStatusGuardRequireActive(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	guard := OrganizationStatusGuard{Store: s}
	if err := guard.RequireActive(ctx, "legacy-account"); err != nil {
		t.Fatalf("legacy account should be allowed: %v", err)
	}

	now := time.Now().UTC()
	if err := s.SaveSaaSOrganization(ctx, &types.SaaSOrganization{
		AccountID:   "active-account",
		Slug:        "active123",
		Domain:      "active123.cloink.4w.ink",
		DisplayName: "Active",
		Status:      types.SaaSOrganizationStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSOrganization(ctx, &types.SaaSOrganization{
		AccountID:   "suspended-account",
		Slug:        "suspend1",
		Domain:      "suspend1.cloink.4w.ink",
		DisplayName: "Suspended",
		Status:      types.SaaSOrganizationStatusSuspended,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := guard.RequireActive(ctx, "active-account"); err != nil {
		t.Fatalf("active account should be allowed: %v", err)
	}
	if err := guard.RequireActive(ctx, "suspended-account"); err == nil {
		t.Fatal("suspended account should be blocked")
	}
}
