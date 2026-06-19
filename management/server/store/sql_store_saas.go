package store

import (
	"context"
	"errors"
	"time"

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

func (s *SqlStore) UpdateSaaSOrganizationDomain(ctx context.Context, accountID, slug, domain string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conflict types.SaaSOrganization
		if err := tx.
			Where("account_id <> ? AND (domain = ? OR slug = ?)", accountID, domain, slug).
			First(&conflict).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return status.Errorf(status.Internal, "failed to validate saas organization domain")
		} else if err == nil {
			return status.Errorf(status.AlreadyExists, "organization domain already exists")
		}

		orgResult := tx.Model(&types.SaaSOrganization{}).
			Where("account_id = ?", accountID).
			Updates(map[string]any{
				"slug":       slug,
				"domain":     domain,
				"updated_at": time.Now().UTC(),
			})
		if orgResult.Error != nil {
			return status.Errorf(status.Internal, "failed to update saas organization domain")
		}
		if orgResult.RowsAffected == 0 {
			return status.Errorf(status.NotFound, "saas organization not found")
		}

		accountResult := tx.Model(&types.Account{}).
			Where("id = ?", accountID).
			Updates(map[string]any{
				"domain":                    domain,
				"settings_dns_domain":       domain,
				"domain_category":           types.PrivateCategory,
				"is_domain_primary_account": true,
			})
		if accountResult.Error != nil {
			return status.Errorf(status.Internal, "failed to update account domain")
		}
		if accountResult.RowsAffected == 0 {
			return status.Errorf(status.NotFound, "account not found")
		}

		return nil
	})
}

func (s *SqlStore) DeleteSaaSDataByAccountID(ctx context.Context, accountID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		models := []any{
			&types.SaaSOrgMenuVisibility{},
			&types.SaaSTrafficLedger{},
			&types.SaaSAutoRenewalAttempt{},
			&types.SaaSOfflinePaymentRecord{},
			&types.SaaSInvoiceRequest{},
			&types.SaaSReconciliationRecord{},
			&types.SaaSPaymentRefund{},
			&types.SaaSBillItem{},
			&types.SaaSBill{},
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

func (s *SqlStore) SaveSaaSBill(ctx context.Context, bill *types.SaaSBill) error {
	return saveSaaSModel(ctx, s.db, bill, "saas bill")
}

func (s *SqlStore) GetSaaSBill(ctx context.Context, lockStrength LockingStrength, billID string) (*types.SaaSBill, error) {
	var bill types.SaaSBill
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.First(&bill, "id = ?", billID).Error; err != nil {
		return nil, saasNotFoundOrInternal(err, "saas bill not found")
	}
	return &bill, nil
}

func (s *SqlStore) GetSaaSBillByPaymentOrderID(ctx context.Context, lockStrength LockingStrength, paymentOrderID string) (*types.SaaSBill, error) {
	var bill types.SaaSBill
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.First(&bill, "payment_order_id = ?", paymentOrderID).Error; err != nil {
		return nil, saasNotFoundOrInternal(err, "saas bill not found")
	}
	return &bill, nil
}

func (s *SqlStore) ListSaaSBills(ctx context.Context, lockStrength LockingStrength, accountID string) ([]*types.SaaSBill, error) {
	var bills []*types.SaaSBill
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.
		Where("account_id = ?", accountID).
		Order("created_at DESC").
		Find(&bills).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to list saas bills")
	}
	return bills, nil
}

func (s *SqlStore) SaveSaaSBillItem(ctx context.Context, item *types.SaaSBillItem) error {
	return saveSaaSModel(ctx, s.db, item, "saas bill item")
}

func (s *SqlStore) ListSaaSBillItems(ctx context.Context, lockStrength LockingStrength, billID string) ([]*types.SaaSBillItem, error) {
	var items []*types.SaaSBillItem
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.
		Where("bill_id = ?", billID).
		Order("created_at ASC").
		Find(&items).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to list saas bill items")
	}
	return items, nil
}

func (s *SqlStore) SaveSaaSPaymentRefund(ctx context.Context, refund *types.SaaSPaymentRefund) error {
	return saveSaaSModel(ctx, s.db, refund, "saas payment refund")
}

func (s *SqlStore) ListSaaSPaymentRefunds(ctx context.Context, lockStrength LockingStrength, accountID string) ([]*types.SaaSPaymentRefund, error) {
	var refunds []*types.SaaSPaymentRefund
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.
		Where("account_id = ?", accountID).
		Order("created_at DESC").
		Find(&refunds).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to list saas payment refunds")
	}
	return refunds, nil
}

func (s *SqlStore) SaveSaaSReconciliationRecord(ctx context.Context, record *types.SaaSReconciliationRecord) error {
	return saveSaaSModel(ctx, s.db, record, "saas reconciliation record")
}

func (s *SqlStore) ListSaaSReconciliationRecords(ctx context.Context, lockStrength LockingStrength, accountID string) ([]*types.SaaSReconciliationRecord, error) {
	var records []*types.SaaSReconciliationRecord
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.
		Where("account_id = ?", accountID).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to list saas reconciliation records")
	}
	return records, nil
}

func (s *SqlStore) SaveSaaSInvoiceRequest(ctx context.Context, invoice *types.SaaSInvoiceRequest) error {
	return saveSaaSModel(ctx, s.db, invoice, "saas invoice request")
}

func (s *SqlStore) ListSaaSInvoiceRequests(ctx context.Context, lockStrength LockingStrength, accountID string) ([]*types.SaaSInvoiceRequest, error) {
	var invoices []*types.SaaSInvoiceRequest
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.
		Where("account_id = ?", accountID).
		Order("created_at DESC").
		Find(&invoices).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to list saas invoice requests")
	}
	return invoices, nil
}

func (s *SqlStore) SaveSaaSOfflinePaymentRecord(ctx context.Context, record *types.SaaSOfflinePaymentRecord) error {
	return saveSaaSModel(ctx, s.db, record, "saas offline payment record")
}

func (s *SqlStore) ListSaaSOfflinePaymentRecords(ctx context.Context, lockStrength LockingStrength, accountID string) ([]*types.SaaSOfflinePaymentRecord, error) {
	var records []*types.SaaSOfflinePaymentRecord
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.
		Where("account_id = ?", accountID).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to list saas offline payment records")
	}
	return records, nil
}

func (s *SqlStore) SaveSaaSAutoRenewalAttempt(ctx context.Context, attempt *types.SaaSAutoRenewalAttempt) error {
	return saveSaaSModel(ctx, s.db, attempt, "saas auto renewal attempt")
}

func (s *SqlStore) ListSaaSAutoRenewalAttempts(ctx context.Context, lockStrength LockingStrength, accountID string) ([]*types.SaaSAutoRenewalAttempt, error) {
	var attempts []*types.SaaSAutoRenewalAttempt
	tx := applyLockingStrength(s.db.WithContext(ctx), lockStrength)
	if err := tx.
		Where("account_id = ?", accountID).
		Order("created_at DESC").
		Find(&attempts).Error; err != nil {
		return nil, status.Errorf(status.Internal, "failed to list saas auto renewal attempts")
	}
	return attempts, nil
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
