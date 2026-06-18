package saas

import (
	"context"
	"testing"

	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestBuildOrganizationDomain(t *testing.T) {
	domain, err := BuildOrganizationDomain("Team1234", ".cloink.4w.ink.")
	if err != nil {
		t.Fatal(err)
	}
	if domain != "team1234.cloink.4w.ink" {
		t.Fatalf("unexpected domain: %s", domain)
	}

	if _, err := BuildOrganizationDomain("admin", "cloink.4w.ink"); err == nil {
		t.Fatal("expected reserved slug error")
	}
	if _, err := BuildOrganizationDomain("bad-slug", "cloink.4w.ink"); err == nil {
		t.Fatal("expected invalid slug error")
	}
	if _, err := BuildOrganizationDomain("team1234", "bad suffix"); err == nil {
		t.Fatal("expected invalid suffix error")
	}
}

func TestDomainAllocatorAllocate(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	existing := &types.SaaSOrganization{
		AccountID:   "account-a",
		Slug:        "existing1",
		Domain:      "existing1.cloink.4w.ink",
		DisplayName: "Existing",
		Status:      types.SaaSOrganizationStatusActive,
	}
	if err := s.SaveSaaSOrganization(ctx, existing); err != nil {
		t.Fatal(err)
	}

	allocator := DomainAllocator{
		Store:   s,
		Suffix:  "cloink.4w.ink",
		MinSize: 8,
		MaxSize: 12,
	}

	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		allocated, err := allocator.Allocate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !ValidSlug(allocated.Slug) {
			t.Fatalf("invalid slug: %s", allocated.Slug)
		}
		if IsReservedSlug(allocated.Slug) {
			t.Fatalf("reserved slug allocated: %s", allocated.Slug)
		}
		if allocated.Domain == existing.Domain {
			t.Fatalf("allocated existing domain: %s", allocated.Domain)
		}
		if _, ok := seen[allocated.Slug]; ok {
			t.Fatalf("duplicate slug allocated: %s", allocated.Slug)
		}
		seen[allocated.Slug] = struct{}{}
	}
}

func TestDomainAllocatorRequiresSuffix(t *testing.T) {
	if _, err := (DomainAllocator{}).Allocate(context.Background()); err != ErrDomainSuffixRequired {
		t.Fatalf("expected ErrDomainSuffixRequired, got %v", err)
	}
}
