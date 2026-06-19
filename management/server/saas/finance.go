package saas

import (
	"context"
	"strings"
	"time"

	"github.com/rs/xid"

	"github.com/netbirdio/netbird/management/server/activity"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

type FinanceService struct {
	Store    store.Store
	Audit    AuditRecorder
	Notifier SubscriptionNotifier
	Now      func() time.Time
}

type InvoiceRequestInput struct {
	AccountID    string
	BillID       string
	InvoiceTitle string
	TaxID        string
	Email        string
	AmountCents  int64
	Currency     string
	OperatorID   string
}

type InvoiceUpdateInput struct {
	AccountID  string
	InvoiceID  string
	Status     string
	InvoiceNo  string
	InvoiceURL string
	Reason     string
	OperatorID string
}

type OfflinePaymentInput struct {
	AccountID       string
	Provider        string
	ProviderTradeNo string
	AmountCents     int64
	Currency        string
	Status          string
	Plan            string
	PeriodDays      int
	HighSpeedGB     int
	PaymentOrderID  string
	BillID          string
	Note            string
	OperatorID      string
	Payload         map[string]any
}

type AutoRenewalAttemptInput struct {
	AccountID      string
	Provider       string
	AmountCents    int64
	Currency       string
	Status         string
	PaymentOrderID string
	Reason         string
	NextRetryAt    *time.Time
	Payload        map[string]any
}

func (s FinanceService) RequestInvoice(ctx context.Context, req InvoiceRequestInput) (*types.SaaSInvoiceRequest, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if req.AccountID == "" || strings.TrimSpace(req.InvoiceTitle) == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id and invoice title are required")
	}
	if req.Currency == "" {
		req.Currency = "CNY"
	}
	if req.AmountCents <= 0 && req.BillID != "" {
		bill, err := s.Store.GetSaaSBill(ctx, store.LockingStrengthNone, req.BillID)
		if err != nil {
			return nil, err
		}
		if bill.AccountID != req.AccountID {
			return nil, status.Errorf(status.NotFound, "saas bill not found")
		}
		req.AmountCents = bill.TotalCents
		req.Currency = bill.Currency
	}
	if req.AmountCents <= 0 {
		return nil, status.Errorf(status.InvalidArgument, "amount cents must be positive")
	}
	now := s.now()
	invoice := &types.SaaSInvoiceRequest{
		ID:           xid.New().String(),
		AccountID:    req.AccountID,
		BillID:       req.BillID,
		InvoiceTitle: strings.TrimSpace(req.InvoiceTitle),
		TaxID:        strings.TrimSpace(req.TaxID),
		Email:        strings.TrimSpace(req.Email),
		AmountCents:  req.AmountCents,
		Currency:     req.Currency,
		Status:       types.SaaSInvoiceStatusRequested,
		OperatorID:   req.OperatorID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.Store.SaveSaaSInvoiceRequest(ctx, invoice); err != nil {
		return nil, err
	}
	s.Audit.Record(ctx, req.OperatorID, invoice.ID, req.AccountID, activity.SaaSInvoiceRequested, map[string]any{
		"bill_id":      invoice.BillID,
		"amount_cents": invoice.AmountCents,
		"status":       invoice.Status,
	})
	return invoice, nil
}

func (s FinanceService) UpdateInvoice(ctx context.Context, req InvoiceUpdateInput) (*types.SaaSInvoiceRequest, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if req.AccountID == "" || req.InvoiceID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id and invoice id are required")
	}
	if req.Status == "" {
		req.Status = types.SaaSInvoiceStatusIssued
	}
	if !allowedInvoiceStatus(req.Status) {
		return nil, status.Errorf(status.InvalidArgument, "invalid invoice status")
	}
	invoices, err := s.Store.ListSaaSInvoiceRequests(ctx, store.LockingStrengthNone, req.AccountID)
	if err != nil {
		return nil, err
	}
	var invoice *types.SaaSInvoiceRequest
	for _, item := range invoices {
		if item.ID == req.InvoiceID {
			invoice = item
			break
		}
	}
	if invoice == nil {
		return nil, status.Errorf(status.NotFound, "saas invoice request not found")
	}
	invoice.Status = req.Status
	invoice.InvoiceNo = strings.TrimSpace(req.InvoiceNo)
	invoice.InvoiceURL = strings.TrimSpace(req.InvoiceURL)
	invoice.Reason = req.Reason
	invoice.OperatorID = req.OperatorID
	invoice.UpdatedAt = s.now()
	if err := s.Store.SaveSaaSInvoiceRequest(ctx, invoice); err != nil {
		return nil, err
	}
	s.Audit.Record(ctx, req.OperatorID, invoice.ID, req.AccountID, activity.SaaSInvoiceUpdated, map[string]any{
		"status":     invoice.Status,
		"invoice_no": invoice.InvoiceNo,
		"reason":     invoice.Reason,
	})
	return invoice, nil
}

func (s FinanceService) RecordOfflinePayment(ctx context.Context, req OfflinePaymentInput) (*types.SaaSOfflinePaymentRecord, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if req.AccountID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id is required")
	}
	if req.Provider == "" {
		req.Provider = types.SaaSPaymentProviderBank
	}
	if !allowedFinanceProvider(req.Provider) {
		return nil, status.Errorf(status.InvalidArgument, "invalid payment provider")
	}
	if req.Currency == "" {
		req.Currency = "CNY"
	}
	if req.Status == "" {
		req.Status = types.SaaSOfflinePaymentStatusApplied
	}
	if !allowedOfflinePaymentStatus(req.Status) {
		return nil, status.Errorf(status.InvalidArgument, "invalid offline payment status")
	}
	if req.AmountCents <= 0 {
		return nil, status.Errorf(status.InvalidArgument, "amount cents must be positive")
	}
	now := s.now()
	record := &types.SaaSOfflinePaymentRecord{
		ID:                    xid.New().String(),
		AccountID:             req.AccountID,
		Provider:              req.Provider,
		ProviderTradeNo:       req.ProviderTradeNo,
		AmountCents:           req.AmountCents,
		Currency:              req.Currency,
		Status:                req.Status,
		Plan:                  req.Plan,
		PeriodDays:            req.PeriodDays,
		HighSpeedTrafficBytes: int64(req.HighSpeedGB) * 1024 * 1024 * 1024,
		PaymentOrderID:        req.PaymentOrderID,
		BillID:                req.BillID,
		Note:                  req.Note,
		OperatorID:            req.OperatorID,
		Payload:               req.Payload,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := s.Store.SaveSaaSOfflinePaymentRecord(ctx, record); err != nil {
		return nil, err
	}
	if req.Status == types.SaaSOfflinePaymentStatusApplied {
		_, err := (SubscriptionLifecycleService{
			Store:    s.Store,
			Audit:    s.Audit,
			Notifier: s.Notifier,
			Now:      s.Now,
		}).Renew(ctx, RenewSubscriptionRequest{
			AccountID:      req.AccountID,
			Plan:           req.Plan,
			PeriodDays:     req.PeriodDays,
			HighSpeedGB:    req.HighSpeedGB,
			OperatorID:     req.OperatorID,
			Reason:         "offline payment applied",
			PaymentOrderID: req.PaymentOrderID,
		})
		if err != nil {
			return nil, err
		}
	}
	s.Audit.Record(ctx, req.OperatorID, record.ID, req.AccountID, activity.SaaSOfflinePaymentRecorded, map[string]any{
		"provider":       record.Provider,
		"amount_cents":   record.AmountCents,
		"status":         record.Status,
		"period_days":    record.PeriodDays,
		"high_speed_gb":  req.HighSpeedGB,
		"payment_order":  record.PaymentOrderID,
		"provider_trade": record.ProviderTradeNo,
	})
	return record, nil
}

func (s FinanceService) RecordAutoRenewalAttempt(ctx context.Context, req AutoRenewalAttemptInput) (*types.SaaSAutoRenewalAttempt, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if req.AccountID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id is required")
	}
	if req.Provider == "" {
		req.Provider = types.SaaSPaymentProviderAlipay
	}
	if !allowedFinanceProvider(req.Provider) {
		return nil, status.Errorf(status.InvalidArgument, "invalid payment provider")
	}
	if req.Currency == "" {
		req.Currency = "CNY"
	}
	if req.Status == "" {
		req.Status = types.SaaSAutoRenewalAttemptStatusFailed
	}
	if req.Status != types.SaaSAutoRenewalAttemptStatusSucceeded && req.Status != types.SaaSAutoRenewalAttemptStatusFailed {
		return nil, status.Errorf(status.InvalidArgument, "invalid auto renewal attempt status")
	}
	now := s.now()
	attempt := &types.SaaSAutoRenewalAttempt{
		ID:             xid.New().String(),
		AccountID:      req.AccountID,
		Provider:       req.Provider,
		AmountCents:    req.AmountCents,
		Currency:       req.Currency,
		Status:         req.Status,
		PaymentOrderID: req.PaymentOrderID,
		Reason:         req.Reason,
		NextRetryAt:    req.NextRetryAt,
		Payload:        req.Payload,
		AttemptedAt:    now,
		CreatedAt:      now,
	}
	if err := s.Store.SaveSaaSAutoRenewalAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	if req.Status == types.SaaSAutoRenewalAttemptStatusFailed {
		subscription, err := s.Store.GetSaaSSubscription(ctx, store.LockingStrengthUpdate, req.AccountID)
		if err != nil {
			return nil, err
		}
		if subscription.Status == types.SaaSSubscriptionStatusActive {
			subscription.Status = types.SaaSSubscriptionStatusPastDue
			subscription.UpdatedAt = now
			if err := s.Store.SaveSaaSSubscription(ctx, subscription); err != nil {
				return nil, err
			}
			notifier := s.Notifier
			if notifier.Store == nil {
				notifier.Store = s.Store
			}
			notifier.NotifyChanged(ctx, req.AccountID)
		}
	}
	s.Audit.Record(ctx, activity.SystemInitiator, attempt.ID, req.AccountID, activity.SaaSAutoRenewalAttempted, map[string]any{
		"provider":      attempt.Provider,
		"amount_cents":  attempt.AmountCents,
		"status":        attempt.Status,
		"reason":        attempt.Reason,
		"payment_order": attempt.PaymentOrderID,
		"next_retry_at": formatTimeForMeta(attempt.NextRetryAt),
	})
	return attempt, nil
}

func (s FinanceService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func allowedInvoiceStatus(statusValue string) bool {
	switch statusValue {
	case types.SaaSInvoiceStatusRequested, types.SaaSInvoiceStatusIssued, types.SaaSInvoiceStatusRejected:
		return true
	default:
		return false
	}
}

func allowedOfflinePaymentStatus(statusValue string) bool {
	switch statusValue {
	case types.SaaSOfflinePaymentStatusPending, types.SaaSOfflinePaymentStatusApplied, types.SaaSOfflinePaymentStatusRejected:
		return true
	default:
		return false
	}
}

func allowedFinanceProvider(provider string) bool {
	switch provider {
	case types.SaaSPaymentProviderAlipay, types.SaaSPaymentProviderWeChat, types.SaaSPaymentProviderBank:
		return true
	default:
		return false
	}
}

func formatTimeForMeta(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
