package store

import (
	"context"

	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

func errSaaSUnsupported() error {
	return status.Errorf(status.Internal, "saas store is not supported by file store")
}

func (s *FileStore) SaveSaaSOrganization(ctx context.Context, organization *types.SaaSOrganization) error {
	return errSaaSUnsupported()
}

func (s *FileStore) GetSaaSOrganizationByAccountID(ctx context.Context, lockStrength LockingStrength, accountID string) (*types.SaaSOrganization, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) GetSaaSOrganizationByDomain(ctx context.Context, lockStrength LockingStrength, domain string) (*types.SaaSOrganization, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) ListSaaSOrganizations(ctx context.Context, lockStrength LockingStrength) ([]*types.SaaSOrganization, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) DeleteSaaSDataByAccountID(ctx context.Context, accountID string) error {
	return errSaaSUnsupported()
}

func (s *FileStore) SaveSaaSSubscription(ctx context.Context, subscription *types.SaaSSubscription) error {
	return errSaaSUnsupported()
}

func (s *FileStore) GetSaaSSubscription(ctx context.Context, lockStrength LockingStrength, accountID string) (*types.SaaSSubscription, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) SaveSaaSBandwidthPolicy(ctx context.Context, policy *types.SaaSBandwidthPolicy) error {
	return errSaaSUnsupported()
}

func (s *FileStore) GetSaaSBandwidthPolicy(ctx context.Context, lockStrength LockingStrength, accountID string) (*types.SaaSBandwidthPolicy, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) SaveSaaSPlatformAdmin(ctx context.Context, admin *types.SaaSPlatformAdmin) error {
	return errSaaSUnsupported()
}

func (s *FileStore) GetSaaSPlatformAdmin(ctx context.Context, lockStrength LockingStrength, userID string) (*types.SaaSPlatformAdmin, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) SaveSaaSOrgMenuVisibility(ctx context.Context, item *types.SaaSOrgMenuVisibility) error {
	return errSaaSUnsupported()
}

func (s *FileStore) GetSaaSOrgMenuVisibility(ctx context.Context, lockStrength LockingStrength, accountID string) ([]*types.SaaSOrgMenuVisibility, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) CreateSaaSTrafficLedger(ctx context.Context, entry *types.SaaSTrafficLedger) error {
	return errSaaSUnsupported()
}

func (s *FileStore) GetSaaSTrafficLedgerForPeriod(ctx context.Context, lockStrength LockingStrength, accountID, periodKey string) ([]*types.SaaSTrafficLedger, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) SaveSaaSTrafficPurchase(ctx context.Context, purchase *types.SaaSTrafficPurchase) error {
	return errSaaSUnsupported()
}

func (s *FileStore) GetSaaSTrafficPurchase(ctx context.Context, lockStrength LockingStrength, purchaseID string) (*types.SaaSTrafficPurchase, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) SaveSaaSPaymentOrder(ctx context.Context, order *types.SaaSPaymentOrder) error {
	return errSaaSUnsupported()
}

func (s *FileStore) GetSaaSPaymentOrder(ctx context.Context, lockStrength LockingStrength, orderID string) (*types.SaaSPaymentOrder, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) GetSaaSPaymentOrderByProviderTradeNo(ctx context.Context, lockStrength LockingStrength, provider, providerTradeNo string) (*types.SaaSPaymentOrder, error) {
	return nil, errSaaSUnsupported()
}

func (s *FileStore) ListSaaSPaymentOrders(ctx context.Context, lockStrength LockingStrength, accountID string) ([]*types.SaaSPaymentOrder, error) {
	return nil, errSaaSUnsupported()
}
