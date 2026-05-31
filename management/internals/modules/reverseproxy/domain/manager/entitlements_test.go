package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/netbirdio/netbird/management/internals/modules/reverseproxy/domain"
	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestValidateCreateDomainEntitlementsBasicDeniesSecondCustomDomain(t *testing.T) {
	mgr := Manager{
		store: &entitlementsDomainStore{
			domains: []*domain.Domain{{ID: "domain-a", Domain: "one.example.com"}},
		},
		entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider()),
	}

	err := mgr.validateCreateDomainEntitlements(context.Background(), "account-a")
	if err == nil {
		t.Fatalf("expected custom domain limit error")
	}
	if !strings.Contains(err.Error(), "limit_exceeded") || !strings.Contains(err.Error(), string(entitlements.LimitCustomDomains)) {
		t.Fatalf("error = %q, want custom domain limit_exceeded", err)
	}
}

type entitlementsDomainStore struct {
	domains []*domain.Domain
}

func (s *entitlementsDomainStore) GetAccount(context.Context, string) (*types.Account, error) {
	panic("not implemented")
}

func (s *entitlementsDomainStore) GetCustomDomain(context.Context, string, string) (*domain.Domain, error) {
	panic("not implemented")
}

func (s *entitlementsDomainStore) ListFreeDomains(context.Context, string) ([]string, error) {
	panic("not implemented")
}

func (s *entitlementsDomainStore) ListCustomDomains(context.Context, string) ([]*domain.Domain, error) {
	return s.domains, nil
}

func (s *entitlementsDomainStore) CreateCustomDomain(context.Context, string, string, string, bool) (*domain.Domain, error) {
	panic("not implemented")
}

func (s *entitlementsDomainStore) UpdateCustomDomain(context.Context, string, *domain.Domain) (*domain.Domain, error) {
	panic("not implemented")
}

func (s *entitlementsDomainStore) DeleteCustomDomain(context.Context, string, string) error {
	panic("not implemented")
}
