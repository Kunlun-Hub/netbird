package store

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

func (s *SqlStore) SaveSaaSOrganization(ctx context.Context, organization *types.SaaSOrganization) error {
	return saveSaaSModel(ctx, s.db, organization, "saas organization")
}

func (s *SqlStore) GetSaaSOrganizationByAccountID(ctx context.Context, lockStrength LockingStrength, accountID string) (*types.SaaSOrganization, error) {
	var organization types.SaaSOrganization
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.First(&organization, "account_id = ?", accountID).Error; err != nil {
		return nil, saasNotFoundOrInternal(err, "saas organization not found")
	}
	return &organization, nil
}

func (s *SqlStore) GetSaaSOrganizationByDomain(ctx context.Context, lockStrength LockingStrength, domain string) (*types.SaaSOrganization, error) {
	var organization types.SaaSOrganization
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.First(&organization, "domain = ?", domain).Error; err != nil {
		return nil, saasNotFoundOrInternal(err, "saas organization not found")
	}
	return &organization, nil
}

func (s *SqlStore) ListSaaSOrganizations(ctx context.Context, lockStrength LockingStrength) ([]*types.SaaSOrganization, error) {
	var organizations []*types.SaaSOrganization
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.Order("created_at DESC").Find(&organizations).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to list saas organizations")
	}
	return organizations, nil
}

func (s *SqlStore) DeleteSaaSDataByAccountID(ctx context.Context, accountID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		models := []any{
			&types.SaaSOrgMenuVisibility{},
			&types.SaaSTrafficLedger{},
			&types.SaaSTrafficPurchase{},
			&types.SaaSPaymentOrder{},
			&types.SaaSBandwidthPolicy{},
			&types.SaaSSubscription{},
			&types.SaaSOrganization{},
		}
		for _, model := range models {
			if err := tx.Where("account_id = ?", accountID).Delete(model).Error; err != nil {
				return status.Errorf(status.Internal, "failed to delete saas data")
			}
		}
		return nil
	})
}

func (s *SqlStore) SaveSaaSSubscription(ctx context.Context, subscription *types.SaaSSubscription) error {
	return saveSaaSModel(ctx, s.db, subscription, "saas subscription")
}

func (s *SqlStore) GetSaaSSubscription(ctx context.Context, lockStrength LockingStrength, accountID string) (*types.SaaSSubscription, error) {
	var subscription types.SaaSSubscription
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.First(&subscription, "account_id = ?", accountID).Error; err != nil {
		return nil, saasNotFoundOrInternal(err, "saas subscription not found")
	}
	return &subscription, nil
}

func (s *SqlStore) SaveSaaSBandwidthPolicy(ctx context.Context, policy *types.SaaSBandwidthPolicy) error {
	return saveSaaSModel(ctx, s.db, policy, "saas bandwidth policy")
}

func (s *SqlStore) GetSaaSBandwidthPolicy(ctx context.Context, lockStrength LockingStrength, accountID string) (*types.SaaSBandwidthPolicy, error) {
	var policy types.SaaSBandwidthPolicy
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.First(&policy, "account_id = ?", accountID).Error; err != nil {
		return nil, saasNotFoundOrInternal(err, "saas bandwidth policy not found")
	}
	return &policy, nil
}

func (s *SqlStore) SaveSaaSPlatformAdmin(ctx context.Context, admin *types.SaaSPlatformAdmin) error {
	return saveSaaSModel(ctx, s.db, admin, "saas platform admin")
}

func (s *SqlStore) GetSaaSPlatformAdmin(ctx context.Context, lockStrength LockingStrength, userID string) (*types.SaaSPlatformAdmin, error) {
	var admin types.SaaSPlatformAdmin
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.First(&admin, "user_id = ?", userID).Error; err != nil {
		return nil, saasNotFoundOrInternal(err, "saas platform admin not found")
	}
	return &admin, nil
}

func (s *SqlStore) SaveSaaSOrgMenuVisibility(ctx context.Context, item *types.SaaSOrgMenuVisibility) error {
	return saveSaaSModel(ctx, s.db, item, "saas org menu visibility")
}

func (s *SqlStore) GetSaaSOrgMenuVisibility(ctx context.Context, lockStrength LockingStrength, accountID string) ([]*types.SaaSOrgMenuVisibility, error) {
	var items []*types.SaaSOrgMenuVisibility
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.Where("account_id = ?", accountID).Find(&items).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to get saas org menu visibility")
	}
	return items, nil
}

func (s *SqlStore) CreateSaaSTrafficLedger(ctx context.Context, entry *types.SaaSTrafficLedger) error {
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(entry)
	if result.Error != nil {
		return status.Errorf(status.Internal, "failed to create saas traffic ledger")
	}
	return nil
}

func (s *SqlStore) GetSaaSTrafficLedgerForPeriod(ctx context.Context, lockStrength LockingStrength, accountID, periodKey string) ([]*types.SaaSTrafficLedger, error) {
	var entries []*types.SaaSTrafficLedger
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.
		Where("account_id = ? AND period_key = ?", accountID, periodKey).
		Order("recorded_at ASC").
		Find(&entries).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to get saas traffic ledger")
	}
	return entries, nil
}

func (s *SqlStore) SaveSaaSTrafficPurchase(ctx context.Context, purchase *types.SaaSTrafficPurchase) error {
	return saveSaaSModel(ctx, s.db, purchase, "saas traffic purchase")
}

func (s *SqlStore) GetSaaSTrafficPurchase(ctx context.Context, lockStrength LockingStrength, purchaseID string) (*types.SaaSTrafficPurchase, error) {
	var purchase types.SaaSTrafficPurchase
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.First(&purchase, "id = ?", purchaseID).Error; err != nil {
		return nil, saasNotFoundOrInternal(err, "saas traffic purchase not found")
	}
	return &purchase, nil
}

func (s *SqlStore) SaveSaaSPaymentOrder(ctx context.Context, order *types.SaaSPaymentOrder) error {
	return saveSaaSModel(ctx, s.db, order, "saas payment order")
}

func (s *SqlStore) GetSaaSPaymentOrder(ctx context.Context, lockStrength LockingStrength, orderID string) (*types.SaaSPaymentOrder, error) {
	var order types.SaaSPaymentOrder
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.First(&order, "id = ?", orderID).Error; err != nil {
		return nil, saasNotFoundOrInternal(err, "saas payment order not found")
	}
	return &order, nil
}

func (s *SqlStore) GetSaaSPaymentOrderByProviderTradeNo(ctx context.Context, lockStrength LockingStrength, provider, providerTradeNo string) (*types.SaaSPaymentOrder, error) {
	var order types.SaaSPaymentOrder
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.First(&order, "provider = ? AND provider_trade_no = ?", provider, providerTradeNo).Error; err != nil {
		return nil, saasNotFoundOrInternal(err, "saas payment order not found")
	}
	return &order, nil
}

func (s *SqlStore) ListSaaSPaymentOrders(ctx context.Context, lockStrength LockingStrength, accountID string) ([]*types.SaaSPaymentOrder, error) {
	var orders []*types.SaaSPaymentOrder
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.
		Where("account_id = ?", accountID).
		Order("created_at DESC").
		Find(&orders).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to list saas payment orders")
	}
	return orders, nil
}

func saveSaaSModel(ctx context.Context, db *gorm.DB, model any, name string) error {
	if err := db.WithContext(ctx).Save(model).Error; err != nil {
		return status.Errorf(status.Internal, "failed to save %s", name)
	}
	return nil
}

func saasNotFoundOrInternal(err error, message string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return status.Errorf(status.NotFound, "%s", message)
	}
	return status.Errorf(status.Internal, "%s", message)
}

func applyLockingStrength(tx *gorm.DB, lockStrength LockingStrength) *gorm.DB {
	if lockStrength == LockingStrengthNone {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: string(lockStrength)})
}
