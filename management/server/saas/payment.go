package saas

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/xid"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/activity"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

const (
	alipayGatewayURL      = "https://openapi.alipay.com/gateway.do"
	alipaySandboxURL      = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
	alipayTradePagePayAPI = "alipay.trade.page.pay"
)

type PaymentService struct {
	Store  store.Store
	Config nbconfig.SaaSPaymentConfig
	Audit  AuditRecorder
}

type PaymentOrderRequest struct {
	AccountID             string
	PackageType           string
	HighSpeedTrafficBytes int64
	AmountCents           int64
	Subject               string
	Body                  string
	CreatedBy             string
	ReturnURL             string
	ValidFrom             *time.Time
	ValidUntil            *time.Time
}

type PaymentOrderResponse struct {
	Order    *types.SaaSPaymentOrder    `json:"order"`
	Purchase *types.SaaSTrafficPurchase `json:"purchase"`
	PayURL   string                     `json:"pay_url"`
}

type AlipayNotification struct {
	OrderID         string
	ProviderTradeNo string
	TradeStatus     string
	AmountCents     int64
	Payload         map[string]any
}

type RefundRequest struct {
	AccountID       string
	OrderID         string
	RefundTradeNo   string
	AmountCents     int64
	Reason          string
	OperatorID      string
	ProviderPayload map[string]any
}

type RefundResponse struct {
	Refund   *types.SaaSPaymentRefund   `json:"refund"`
	Order    *types.SaaSPaymentOrder    `json:"order"`
	Purchase *types.SaaSTrafficPurchase `json:"purchase"`
	Bill     *types.SaaSBill            `json:"bill"`
}

type ClosePaymentOrderRequest struct {
	AccountID  string
	OrderID    string
	Reason     string
	OperatorID string
}

type ClosePaymentOrderResponse struct {
	Order    *types.SaaSPaymentOrder    `json:"order"`
	Purchase *types.SaaSTrafficPurchase `json:"purchase,omitempty"`
	Bill     *types.SaaSBill            `json:"bill,omitempty"`
}

type ReconciliationRequest struct {
	AccountID         string
	OrderID           string
	Provider          string
	ProviderTradeNo   string
	ActualAmountCents int64
	Currency          string
	Status            string
	Reason            string
	OperatorID        string
	RawPayload        map[string]any
}

type ReconciliationResponse struct {
	Record *types.SaaSReconciliationRecord `json:"record"`
	Order  *types.SaaSPaymentOrder         `json:"order,omitempty"`
}

func (s PaymentService) GetPaymentOrder(ctx context.Context, accountID, orderID string) (*types.SaaSPaymentOrder, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if accountID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id is required")
	}
	if orderID == "" {
		return nil, status.Errorf(status.InvalidArgument, "order id is required")
	}
	order, err := s.Store.GetSaaSPaymentOrder(ctx, store.LockingStrengthNone, orderID)
	if err != nil {
		return nil, err
	}
	if order.AccountID != accountID {
		return nil, status.Errorf(status.NotFound, "saas payment order not found")
	}
	return order, nil
}

func (s PaymentService) RefundPaymentOrder(ctx context.Context, req RefundRequest) (*RefundResponse, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if req.AccountID == "" || req.OrderID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id and order id are required")
	}
	if req.RefundTradeNo == "" {
		req.RefundTradeNo = xid.New().String()
	}
	var order *types.SaaSPaymentOrder
	var purchase *types.SaaSTrafficPurchase
	var bill *types.SaaSBill
	var refund *types.SaaSPaymentRefund
	now := time.Now().UTC()
	err := s.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		lockedOrder, err := tx.GetSaaSPaymentOrder(ctx, store.LockingStrengthUpdate, req.OrderID)
		if err != nil {
			return err
		}
		order = lockedOrder
		if lockedOrder.AccountID != req.AccountID {
			return status.Errorf(status.NotFound, "saas payment order not found")
		}
		if lockedOrder.Status == types.SaaSPaymentOrderStatusRefunded {
			return status.Errorf(status.PreconditionFailed, "payment order is already refunded")
		}
		if lockedOrder.Status != types.SaaSPaymentOrderStatusPaid {
			return status.Errorf(status.PreconditionFailed, "payment order is not refundable")
		}
		if req.AmountCents == 0 {
			req.AmountCents = lockedOrder.AmountCents
		}
		if req.AmountCents != lockedOrder.AmountCents {
			return status.Errorf(status.InvalidArgument, "partial refunds are not supported yet")
		}

		lockedPurchase, err := tx.GetSaaSTrafficPurchase(ctx, store.LockingStrengthUpdate, lockedOrder.PurchaseID)
		if err != nil {
			return err
		}
		purchase = lockedPurchase
		if purchase.AccountID != req.AccountID {
			return status.Errorf(status.InvalidArgument, "payment order purchase account mismatch")
		}

		subscription, err := tx.GetSaaSSubscription(ctx, store.LockingStrengthUpdate, req.AccountID)
		if err != nil {
			return err
		}
		subscription.HighSpeedTrafficBytes -= purchase.HighSpeedTrafficBytes
		if subscription.HighSpeedTrafficBytes < 0 {
			subscription.HighSpeedTrafficBytes = 0
		}
		subscription.UpdatedAt = now
		if err := tx.SaveSaaSSubscription(ctx, subscription); err != nil {
			return err
		}

		lockedBill, err := tx.GetSaaSBillByPaymentOrderID(ctx, store.LockingStrengthUpdate, lockedOrder.ID)
		if err != nil {
			return err
		}
		bill = lockedBill

		lockedOrder.Status = types.SaaSPaymentOrderStatusRefunded
		lockedOrder.UpdatedAt = now
		if err := tx.SaveSaaSPaymentOrder(ctx, lockedOrder); err != nil {
			return err
		}
		purchase.Status = types.SaaSTrafficPurchaseStatusRefunded
		purchase.UpdatedAt = now
		if err := tx.SaveSaaSTrafficPurchase(ctx, purchase); err != nil {
			return err
		}
		bill.Status = types.SaaSBillStatusRefunded
		bill.UpdatedAt = now
		if err := tx.SaveSaaSBill(ctx, bill); err != nil {
			return err
		}

		refund = &types.SaaSPaymentRefund{
			ID:                    xid.New().String(),
			AccountID:             req.AccountID,
			PaymentOrderID:        lockedOrder.ID,
			Provider:              lockedOrder.Provider,
			ProviderTradeNo:       lockedOrder.ProviderTradeNo,
			RefundTradeNo:         req.RefundTradeNo,
			AmountCents:           req.AmountCents,
			Currency:              lockedOrder.Currency,
			Reason:                req.Reason,
			Status:                types.SaaSPaymentRefundStatusSucceeded,
			HighSpeedTrafficBytes: purchase.HighSpeedTrafficBytes,
			OperatorID:            req.OperatorID,
			Payload:               req.ProviderPayload,
			CreatedAt:             now,
			UpdatedAt:             now,
		}
		return tx.SaveSaaSPaymentRefund(ctx, refund)
	})
	if err != nil {
		return nil, err
	}
	s.Audit.Record(ctx, req.OperatorID, refund.ID, req.AccountID, activity.SaaSPaymentRefunded, map[string]any{
		"order_id":                 order.ID,
		"refund_trade_no":          refund.RefundTradeNo,
		"amount_cents":             refund.AmountCents,
		"high_speed_traffic_bytes": refund.HighSpeedTrafficBytes,
		"reason":                   refund.Reason,
	})
	return &RefundResponse{Refund: refund, Order: order, Purchase: purchase, Bill: bill}, nil
}

func (s PaymentService) ClosePaymentOrder(ctx context.Context, req ClosePaymentOrderRequest) (*ClosePaymentOrderResponse, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if req.AccountID == "" || req.OrderID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id and order id are required")
	}

	var order *types.SaaSPaymentOrder
	var purchase *types.SaaSTrafficPurchase
	var bill *types.SaaSBill
	now := time.Now().UTC()
	err := s.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		lockedOrder, err := tx.GetSaaSPaymentOrder(ctx, store.LockingStrengthUpdate, req.OrderID)
		if err != nil {
			return err
		}
		order = lockedOrder
		if lockedOrder.AccountID != req.AccountID {
			return status.Errorf(status.NotFound, "saas payment order not found")
		}
		if lockedOrder.Status == types.SaaSPaymentOrderStatusClosed {
			return nil
		}
		if lockedOrder.Status != types.SaaSPaymentOrderStatusPending && lockedOrder.Status != types.SaaSPaymentOrderStatusFailed {
			return status.Errorf(status.PreconditionFailed, "payment order cannot be closed")
		}

		lockedOrder.Status = types.SaaSPaymentOrderStatusClosed
		lockedOrder.UpdatedAt = now
		if err := tx.SaveSaaSPaymentOrder(ctx, lockedOrder); err != nil {
			return err
		}

		if lockedOrder.PurchaseID != "" {
			lockedPurchase, err := tx.GetSaaSTrafficPurchase(ctx, store.LockingStrengthUpdate, lockedOrder.PurchaseID)
			if err != nil {
				return err
			}
			purchase = lockedPurchase
			if lockedPurchase.AccountID != req.AccountID {
				return status.Errorf(status.InvalidArgument, "payment order purchase account mismatch")
			}
			if lockedPurchase.Status == types.SaaSTrafficPurchaseStatusPending {
				lockedPurchase.Status = types.SaaSTrafficPurchaseStatusRefunded
				lockedPurchase.UpdatedAt = now
				if err := tx.SaveSaaSTrafficPurchase(ctx, lockedPurchase); err != nil {
					return err
				}
			}
		}

		lockedBill, err := tx.GetSaaSBillByPaymentOrderID(ctx, store.LockingStrengthUpdate, lockedOrder.ID)
		if err != nil {
			return err
		}
		bill = lockedBill
		if lockedBill.AccountID != req.AccountID {
			return status.Errorf(status.InvalidArgument, "payment order bill account mismatch")
		}
		if lockedBill.Status == types.SaaSBillStatusOpen {
			lockedBill.Status = types.SaaSBillStatusVoid
			lockedBill.UpdatedAt = now
			if err := tx.SaveSaaSBill(ctx, lockedBill); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Audit.Record(ctx, req.OperatorID, req.OrderID, req.AccountID, activity.SaaSPaymentOrderClosed, map[string]any{
		"order_id": req.OrderID,
		"reason":   req.Reason,
	})
	return &ClosePaymentOrderResponse{Order: order, Purchase: purchase, Bill: bill}, nil
}

func (s PaymentService) RecordReconciliation(ctx context.Context, req ReconciliationRequest) (*ReconciliationResponse, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if req.Provider == "" {
		req.Provider = types.SaaSPaymentProviderAlipay
	}
	if req.Currency == "" {
		req.Currency = "CNY"
	}
	if req.Status == "" {
		req.Status = types.SaaSReconciliationStatusMatched
	}
	if !allowedReconciliationStatus(req.Status) {
		return nil, status.Errorf(status.InvalidArgument, "invalid reconciliation status")
	}

	var order *types.SaaSPaymentOrder
	expectedAmount := int64(0)
	accountID := req.AccountID
	if req.OrderID != "" {
		lockedOrder, err := s.Store.GetSaaSPaymentOrder(ctx, store.LockingStrengthNone, req.OrderID)
		if err != nil {
			return nil, err
		}
		order = lockedOrder
		expectedAmount = lockedOrder.AmountCents
		if accountID == "" {
			accountID = lockedOrder.AccountID
		}
		if accountID != lockedOrder.AccountID {
			return nil, status.Errorf(status.NotFound, "saas payment order not found")
		}
		if req.ProviderTradeNo == "" {
			req.ProviderTradeNo = lockedOrder.ProviderTradeNo
		}
		if req.ActualAmountCents == 0 {
			req.ActualAmountCents = lockedOrder.AmountCents
		}
	}
	if accountID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id is required")
	}
	if req.Status == types.SaaSReconciliationStatusMatched && expectedAmount != 0 && req.ActualAmountCents != expectedAmount {
		req.Status = types.SaaSReconciliationStatusMismatch
		if req.Reason == "" {
			req.Reason = "amount mismatch"
		}
	}
	now := time.Now().UTC()
	record := &types.SaaSReconciliationRecord{
		ID:                  xid.New().String(),
		AccountID:           accountID,
		PaymentOrderID:      req.OrderID,
		Provider:            req.Provider,
		ProviderTradeNo:     req.ProviderTradeNo,
		ExpectedAmountCents: expectedAmount,
		ActualAmountCents:   req.ActualAmountCents,
		Currency:            req.Currency,
		Status:              req.Status,
		Reason:              req.Reason,
		OperatorID:          req.OperatorID,
		RawPayload:          req.RawPayload,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.Store.SaveSaaSReconciliationRecord(ctx, record); err != nil {
		return nil, err
	}
	s.Audit.Record(ctx, req.OperatorID, record.ID, accountID, activity.SaaSPaymentReconciled, map[string]any{
		"order_id":              record.PaymentOrderID,
		"provider_trade_no":     record.ProviderTradeNo,
		"expected_amount_cents": record.ExpectedAmountCents,
		"actual_amount_cents":   record.ActualAmountCents,
		"status":                record.Status,
		"reason":                record.Reason,
	})
	return &ReconciliationResponse{Record: record, Order: order}, nil
}

func allowedReconciliationStatus(statusValue string) bool {
	switch statusValue {
	case types.SaaSReconciliationStatusMatched,
		types.SaaSReconciliationStatusMismatch,
		types.SaaSReconciliationStatusMissing:
		return true
	default:
		return false
	}
}

func (s PaymentService) CreateAlipayOrder(ctx context.Context, req PaymentOrderRequest) (*PaymentOrderResponse, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if s.Config.Provider != "" && s.Config.Provider != types.SaaSPaymentProviderAlipay {
		return nil, status.Errorf(status.PreconditionFailed, "unsupported payment provider")
	}
	if req.AccountID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id is required")
	}
	if req.HighSpeedTrafficBytes <= 0 {
		return nil, status.Errorf(status.InvalidArgument, "high speed traffic bytes must be positive")
	}
	if req.AmountCents <= 0 {
		return nil, status.Errorf(status.InvalidArgument, "amount cents must be positive")
	}
	if s.Config.Alipay.AppID == "" || s.Config.Alipay.AppPrivateKeyFile == "" {
		return nil, status.Errorf(status.PreconditionFailed, "alipay app id and private key are required")
	}
	if req.PackageType == "" {
		req.PackageType = types.SaaSTrafficPurchasePackageOneTime
	}
	if req.Subject == "" {
		req.Subject = "Cloink high-speed traffic package"
	}
	if req.ReturnURL == "" {
		req.ReturnURL = s.Config.Alipay.ReturnURL
	}

	now := time.Now().UTC()
	purchaseID := xid.New().String()
	orderID := xid.New().String()
	purchase := &types.SaaSTrafficPurchase{
		ID:                    purchaseID,
		AccountID:             req.AccountID,
		PackageType:           req.PackageType,
		HighSpeedTrafficBytes: req.HighSpeedTrafficBytes,
		AmountCents:           req.AmountCents,
		Currency:              "CNY",
		PaymentProvider:       types.SaaSPaymentProviderAlipay,
		Status:                types.SaaSTrafficPurchaseStatusPending,
		ValidFrom:             req.ValidFrom,
		ValidUntil:            req.ValidUntil,
		CreatedBy:             req.CreatedBy,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	order := &types.SaaSPaymentOrder{
		ID:          orderID,
		AccountID:   req.AccountID,
		Provider:    types.SaaSPaymentProviderAlipay,
		Subject:     req.Subject,
		Body:        req.Body,
		AmountCents: req.AmountCents,
		Currency:    "CNY",
		Status:      types.SaaSPaymentOrderStatusPending,
		PurchaseID:  purchaseID,
		CreatedBy:   req.CreatedBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	bill := newTrafficPackageBill(order, purchase, now)
	billItem := newTrafficPackageBillItem(bill, purchase, order.Subject, now)

	payURL, err := s.buildAlipayPagePayURL(order, req.ReturnURL)
	if err != nil {
		return nil, err
	}
	order.PayURL = payURL

	if err := s.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		if _, err := tx.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthUpdate, req.AccountID); err != nil {
			return err
		}
		if err := tx.SaveSaaSTrafficPurchase(ctx, purchase); err != nil {
			return err
		}
		if err := tx.SaveSaaSPaymentOrder(ctx, order); err != nil {
			return err
		}
		if err := tx.SaveSaaSBill(ctx, bill); err != nil {
			return err
		}
		return tx.SaveSaaSBillItem(ctx, billItem)
	}); err != nil {
		return nil, err
	}

	s.Audit.Record(ctx, req.CreatedBy, order.ID, req.AccountID, activity.SaaSPaymentOrderCreated, map[string]any{
		"purchase_id":              purchase.ID,
		"package_type":             purchase.PackageType,
		"high_speed_traffic_bytes": purchase.HighSpeedTrafficBytes,
		"amount_cents":             order.AmountCents,
		"provider":                 order.Provider,
		"status":                   order.Status,
	})

	return &PaymentOrderResponse{Order: order, Purchase: purchase, PayURL: payURL}, nil
}

func (s PaymentService) ParseAndVerifyAlipayNotification(values url.Values) (*AlipayNotification, error) {
	if values.Get("app_id") != s.Config.Alipay.AppID {
		return nil, status.Errorf(status.PermissionDenied, "invalid alipay app id")
	}
	if values.Get("sign_type") != "RSA2" {
		return nil, status.Errorf(status.InvalidArgument, "unsupported alipay sign type")
	}
	if err := verifyAlipaySignature(values, s.Config.Alipay.AlipayPublicCertFile); err != nil {
		return nil, err
	}

	amountCents, err := parseAlipayAmountCents(values.Get("total_amount"))
	if err != nil {
		return nil, err
	}
	payload := make(map[string]any, len(values))
	for key, value := range values {
		if len(value) == 1 {
			payload[key] = value[0]
			continue
		}
		payload[key] = value
	}

	return &AlipayNotification{
		OrderID:         values.Get("out_trade_no"),
		ProviderTradeNo: values.Get("trade_no"),
		TradeStatus:     values.Get("trade_status"),
		AmountCents:     amountCents,
		Payload:         payload,
	}, nil
}

func (s PaymentService) ApplyAlipayNotification(ctx context.Context, notification AlipayNotification) (*types.SaaSPaymentOrder, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if notification.OrderID == "" {
		return nil, status.Errorf(status.InvalidArgument, "order id is required")
	}
	if notification.ProviderTradeNo == "" {
		return nil, status.Errorf(status.InvalidArgument, "alipay trade no is required")
	}
	if notification.TradeStatus != "TRADE_SUCCESS" && notification.TradeStatus != "TRADE_FINISHED" {
		return nil, status.Errorf(status.InvalidArgument, "alipay trade status is not successful")
	}

	var order *types.SaaSPaymentOrder
	var purchase *types.SaaSTrafficPurchase
	var bill *types.SaaSBill
	var applied bool
	now := time.Now().UTC()
	err := s.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		lockedOrder, err := tx.GetSaaSPaymentOrder(ctx, store.LockingStrengthUpdate, notification.OrderID)
		if err != nil {
			return err
		}
		order = lockedOrder
		if lockedOrder.Provider != types.SaaSPaymentProviderAlipay {
			return status.Errorf(status.InvalidArgument, "payment order provider mismatch")
		}
		if lockedOrder.Status == types.SaaSPaymentOrderStatusPaid {
			if lockedOrder.ProviderTradeNo != "" && lockedOrder.ProviderTradeNo != notification.ProviderTradeNo {
				return status.Errorf(status.InvalidArgument, "alipay trade no mismatch")
			}
			return nil
		}
		if lockedOrder.Status != types.SaaSPaymentOrderStatusPending {
			return status.Errorf(status.PreconditionFailed, "payment order is not payable")
		}
		if lockedOrder.AmountCents != notification.AmountCents {
			return status.Errorf(status.InvalidArgument, "alipay amount mismatch")
		}

		lockedPurchase, err := tx.GetSaaSTrafficPurchase(ctx, store.LockingStrengthUpdate, lockedOrder.PurchaseID)
		if err != nil {
			return err
		}
		purchase = lockedPurchase
		if purchase.AccountID != lockedOrder.AccountID {
			return status.Errorf(status.InvalidArgument, "payment order purchase account mismatch")
		}
		if purchase.AmountCents != lockedOrder.AmountCents {
			return status.Errorf(status.InvalidArgument, "payment order purchase amount mismatch")
		}

		subscription, err := tx.GetSaaSSubscription(ctx, store.LockingStrengthUpdate, lockedOrder.AccountID)
		if err != nil {
			return err
		}
		subscription.HighSpeedTrafficBytes += purchase.HighSpeedTrafficBytes
		subscription.UpdatedAt = now
		if err := tx.SaveSaaSSubscription(ctx, subscription); err != nil {
			return err
		}

		lockedOrder.ProviderTradeNo = notification.ProviderTradeNo
		lockedOrder.NotifyPayload = notification.Payload
		lockedOrder.Status = types.SaaSPaymentOrderStatusPaid
		lockedOrder.PaidAt = &now
		lockedOrder.UpdatedAt = now
		if err := tx.SaveSaaSPaymentOrder(ctx, lockedOrder); err != nil {
			return err
		}

		purchase.Status = types.SaaSTrafficPurchaseStatusApplied
		purchase.PaymentTradeNo = notification.ProviderTradeNo
		purchase.UpdatedAt = now
		if err := tx.SaveSaaSTrafficPurchase(ctx, purchase); err != nil {
			return err
		}
		lockedBill, err := tx.GetSaaSBillByPaymentOrderID(ctx, store.LockingStrengthUpdate, lockedOrder.ID)
		if err != nil {
			return err
		}
		bill = lockedBill
		if bill.AccountID != lockedOrder.AccountID {
			return status.Errorf(status.InvalidArgument, "payment order bill account mismatch")
		}
		bill.Status = types.SaaSBillStatusPaid
		bill.PaidAt = &now
		bill.UpdatedAt = now
		if err := tx.SaveSaaSBill(ctx, bill); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil {
		accountID := ""
		orderID := notification.OrderID
		if order != nil {
			accountID = order.AccountID
			orderID = order.ID
		}
		s.Audit.Record(ctx, activity.SystemInitiator, orderID, accountID, activity.SaaSPaymentFailed, map[string]any{
			"provider_trade_no": notification.ProviderTradeNo,
			"trade_status":      notification.TradeStatus,
			"amount_cents":      notification.AmountCents,
			"reason":            err.Error(),
		})
		return nil, err
	}
	if applied && order != nil {
		meta := map[string]any{
			"provider_trade_no": notification.ProviderTradeNo,
			"trade_status":      notification.TradeStatus,
			"amount_cents":      order.AmountCents,
			"purchase_id":       order.PurchaseID,
		}
		if purchase != nil {
			meta["high_speed_traffic_bytes"] = purchase.HighSpeedTrafficBytes
			meta["package_type"] = purchase.PackageType
		}
		if bill != nil {
			meta["bill_id"] = bill.ID
		}
		s.Audit.Record(ctx, activity.SystemInitiator, order.ID, order.AccountID, activity.SaaSPaymentSucceeded, meta)
	}
	return order, nil
}

func newTrafficPackageBill(order *types.SaaSPaymentOrder, purchase *types.SaaSTrafficPurchase, now time.Time) *types.SaaSBill {
	return &types.SaaSBill{
		ID:              xid.New().String(),
		AccountID:       order.AccountID,
		PeriodKey:       now.Format("2006-01"),
		Status:          types.SaaSBillStatusOpen,
		SubtotalCents:   order.AmountCents,
		DiscountCents:   0,
		TaxCents:        0,
		TotalCents:      order.AmountCents,
		Currency:        order.Currency,
		PaymentProvider: order.Provider,
		PaymentOrderID:  order.ID,
		DueAt:           purchase.ValidUntil,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func newTrafficPackageBillItem(bill *types.SaaSBill, purchase *types.SaaSTrafficPurchase, subject string, now time.Time) *types.SaaSBillItem {
	if subject == "" {
		subject = "Cloink high-speed traffic package"
	}
	return &types.SaaSBillItem{
		ID:                    xid.New().String(),
		BillID:                bill.ID,
		AccountID:             bill.AccountID,
		ItemType:              types.SaaSBillItemTypeTrafficPackage,
		Description:           subject,
		Quantity:              1,
		UnitAmountCents:       bill.TotalCents,
		AmountCents:           bill.TotalCents,
		Currency:              bill.Currency,
		TrafficPurchaseID:     purchase.ID,
		HighSpeedTrafficBytes: purchase.HighSpeedTrafficBytes,
		CreatedAt:             now,
	}
}

func (s PaymentService) buildAlipayPagePayURL(order *types.SaaSPaymentOrder, returnURL string) (string, error) {
	privateKey, err := loadRSAPrivateKey(s.Config.Alipay.AppPrivateKeyFile)
	if err != nil {
		return "", err
	}
	gateway := alipayGatewayURL
	if strings.Contains(s.Config.Alipay.AppID, "sandbox") {
		gateway = alipaySandboxURL
	}
	params := url.Values{}
	params.Set("app_id", s.Config.Alipay.AppID)
	params.Set("method", alipayTradePagePayAPI)
	params.Set("format", "JSON")
	params.Set("charset", "utf-8")
	params.Set("sign_type", "RSA2")
	params.Set("timestamp", time.Now().UTC().Format("2006-01-02 15:04:05"))
	params.Set("version", "1.0")
	if s.Config.Alipay.NotifyURL != "" {
		params.Set("notify_url", s.Config.Alipay.NotifyURL)
	}
	if returnURL != "" {
		params.Set("return_url", returnURL)
	}
	bizContent := fmt.Sprintf(`{"out_trade_no":%q,"total_amount":%q,"subject":%q,"body":%q,"product_code":"FAST_INSTANT_TRADE_PAY"}`,
		order.ID, formatAmountCNY(order.AmountCents), order.Subject, order.Body)
	params.Set("biz_content", bizContent)

	signature, err := signAlipayParams(params, privateKey)
	if err != nil {
		return "", err
	}
	params.Set("sign", signature)
	return gateway + "?" + params.Encode(), nil
}

func signAlipayParams(params url.Values, privateKey *rsa.PrivateKey) (string, error) {
	signedContent := canonicalAlipayParams(params)
	hash := sha256.Sum256([]byte(signedContent))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", status.Errorf(status.Internal, "failed to sign alipay order")
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func verifyAlipaySignature(params url.Values, publicKeyFile string) error {
	if publicKeyFile == "" {
		return status.Errorf(status.PreconditionFailed, "alipay public cert is required")
	}
	signatureText := params.Get("sign")
	if signatureText == "" {
		return status.Errorf(status.PermissionDenied, "missing alipay signature")
	}
	signature, err := base64.StdEncoding.DecodeString(signatureText)
	if err != nil {
		return status.Errorf(status.PermissionDenied, "invalid alipay signature encoding")
	}
	publicKey, err := loadRSAPublicKey(publicKeyFile)
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(canonicalAlipayParams(params)))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hash[:], signature); err != nil {
		return status.Errorf(status.PermissionDenied, "invalid alipay signature")
	}
	return nil
}

func canonicalAlipayParams(params url.Values) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		if key == "sign" || key == "sign_type" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := params.Get(key)
		if value == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "&")
}

func loadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, status.Errorf(status.PreconditionFailed, "failed to read alipay private key")
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, status.Errorf(status.PreconditionFailed, "invalid alipay private key")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, status.Errorf(status.PreconditionFailed, "invalid alipay private key")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, status.Errorf(status.PreconditionFailed, "alipay private key is not RSA")
	}
	return key, nil
}

func loadRSAPublicKey(path string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, status.Errorf(status.PreconditionFailed, "failed to read alipay public cert")
	}
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
			if key, ok := cert.PublicKey.(*rsa.PublicKey); ok {
				return key, nil
			}
		}
		if keyAny, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
			if key, ok := keyAny.(*rsa.PublicKey); ok {
				return key, nil
			}
		}
		data = rest
	}
	return nil, status.Errorf(status.PreconditionFailed, "invalid alipay public cert")
}

func parseAlipayAmountCents(amount string) (int64, error) {
	if amount == "" {
		return 0, status.Errorf(status.InvalidArgument, "alipay amount is required")
	}
	parts := strings.Split(amount, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, status.Errorf(status.InvalidArgument, "invalid alipay amount")
	}
	yuan, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, status.Errorf(status.InvalidArgument, "invalid alipay amount")
	}
	cents := int64(0)
	if len(parts) == 2 {
		if len(parts[1]) > 2 {
			return 0, status.Errorf(status.InvalidArgument, "invalid alipay amount")
		}
		fen := parts[1]
		for len(fen) < 2 {
			fen += "0"
		}
		cents, err = strconv.ParseInt(fen, 10, 64)
		if err != nil {
			return 0, status.Errorf(status.InvalidArgument, "invalid alipay amount")
		}
	}
	return yuan*100 + cents, nil
}

func formatAmountCNY(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}
