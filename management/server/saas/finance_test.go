package saas

import (
	"context"
	"testing"
	"time"

	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestFinanceServiceInvoiceRequestAndUpdate(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	seedTrafficAccount(t, ctx, s, now, 100)
	bill := &types.SaaSBill{
		ID:         "bill-a",
		AccountID:  "account-a",
		PeriodKey:  "2026-06",
		Status:     types.SaaSBillStatusPaid,
		TotalCents: 9900,
		Currency:   "CNY",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.SaveSaaSBill(ctx, bill); err != nil {
		t.Fatal(err)
	}

	service := FinanceService{Store: s, Now: func() time.Time { return now }}
	invoice, err := service.RequestInvoice(ctx, InvoiceRequestInput{
		AccountID:    "account-a",
		BillID:       bill.ID,
		InvoiceTitle: "Cloink Ltd.",
		TaxID:        "tax-a",
		Email:        "finance@example.com",
		OperatorID:   "platform-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Status != types.SaaSInvoiceStatusRequested || invoice.AmountCents != bill.TotalCents {
		t.Fatalf("unexpected invoice request: %+v", invoice)
	}

	updated, err := service.UpdateInvoice(ctx, InvoiceUpdateInput{
		AccountID:  "account-a",
		InvoiceID:  invoice.ID,
		Status:     types.SaaSInvoiceStatusIssued,
		InvoiceNo:  "INV-20260619",
		InvoiceURL: "https://example.com/invoice.pdf",
		OperatorID: "platform-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != types.SaaSInvoiceStatusIssued || updated.InvoiceNo != "INV-20260619" {
		t.Fatalf("unexpected invoice update: %+v", updated)
	}
}

func TestFinanceServiceOfflinePaymentRenewsSubscription(t *testing.T) {
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

	record, err := (FinanceService{Store: s, Now: func() time.Time { return now }}).RecordOfflinePayment(ctx, OfflinePaymentInput{
		AccountID:       "account-a",
		Provider:        types.SaaSPaymentProviderBank,
		ProviderTradeNo: "bank-20260619",
		AmountCents:     19900,
		Status:          types.SaaSOfflinePaymentStatusApplied,
		Plan:            "pro",
		PeriodDays:      30,
		HighSpeedGB:     10,
		OperatorID:      "platform-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != types.SaaSOfflinePaymentStatusApplied || record.Provider != types.SaaSPaymentProviderBank {
		t.Fatalf("unexpected offline payment record: %+v", record)
	}
	subscription, err = s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Status != types.SaaSSubscriptionStatusActive || subscription.Plan != "pro" {
		t.Fatalf("expected renewed active subscription, got %+v", subscription)
	}
	if subscription.HighSpeedTrafficBytes != 100+(10*1024*1024*1024) {
		t.Fatalf("unexpected renewed traffic quota: %d", subscription.HighSpeedTrafficBytes)
	}
}

func TestFinanceServiceAutoRenewalFailureMarksPastDue(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	seedTrafficAccount(t, ctx, s, now, 100)
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	subscription.Status = types.SaaSSubscriptionStatusActive
	if err := s.SaveSaaSSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}

	attempt, err := (FinanceService{Store: s, Now: func() time.Time { return now }}).RecordAutoRenewalAttempt(ctx, AutoRenewalAttemptInput{
		AccountID:   "account-a",
		Provider:    types.SaaSPaymentProviderAlipay,
		AmountCents: 19900,
		Status:      types.SaaSAutoRenewalAttemptStatusFailed,
		Reason:      "agreement missing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != types.SaaSAutoRenewalAttemptStatusFailed {
		t.Fatalf("unexpected auto renewal attempt: %+v", attempt)
	}
	subscription, err = s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Status != types.SaaSSubscriptionStatusPastDue {
		t.Fatalf("expected failed auto renewal to mark past_due, got %+v", subscription)
	}
}
