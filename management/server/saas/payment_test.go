package saas

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"testing"
	"time"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestPaymentServiceAlipayOrderAndIdempotentNotification(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	config := testPaymentConfig(t)
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	seedTrafficAccount(t, ctx, s, now, 100)

	service := PaymentService{Store: s, Config: config}
	resp, err := service.CreateAlipayOrder(ctx, PaymentOrderRequest{
		AccountID:             "account-a",
		PackageType:           types.SaaSTrafficPurchasePackageOneTime,
		HighSpeedTrafficBytes: 200,
		AmountCents:           9900,
		Subject:               "100 GB high-speed traffic",
		Body:                  "Cloink traffic package",
		CreatedBy:             "user-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Order.Status != types.SaaSPaymentOrderStatusPending || resp.Purchase.Status != types.SaaSTrafficPurchaseStatusPending || resp.PayURL == "" {
		t.Fatalf("unexpected pending order response: %+v", resp)
	}
	if _, err := url.Parse(resp.PayURL); err != nil {
		t.Fatalf("invalid pay url: %v", err)
	}

	values := signedAlipayNotify(t, config, url.Values{
		"app_id":       {config.Alipay.AppID},
		"out_trade_no": {resp.Order.ID},
		"trade_no":     {"20260618220000000001"},
		"trade_status": {"TRADE_SUCCESS"},
		"total_amount": {"99.00"},
		"sign_type":    {"RSA2"},
	})
	notification, err := service.ParseAndVerifyAlipayNotification(values)
	if err != nil {
		t.Fatal(err)
	}
	order, err := service.ApplyAlipayNotification(ctx, *notification)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != types.SaaSPaymentOrderStatusPaid || order.ProviderTradeNo != "20260618220000000001" || order.PaidAt == nil {
		t.Fatalf("unexpected paid order: %+v", order)
	}

	if _, err := service.ApplyAlipayNotification(ctx, *notification); err != nil {
		t.Fatal(err)
	}
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.HighSpeedTrafficBytes != 300 {
		t.Fatalf("expected quota to be applied once, got %d", subscription.HighSpeedTrafficBytes)
	}
	purchase, err := s.GetSaaSTrafficPurchase(ctx, store.LockingStrengthNone, resp.Purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Status != types.SaaSTrafficPurchaseStatusApplied || purchase.PaymentTradeNo != "20260618220000000001" {
		t.Fatalf("unexpected applied purchase: %+v", purchase)
	}
}

func TestPaymentServiceRejectsInvalidAlipaySignature(t *testing.T) {
	config := testPaymentConfig(t)
	values := signedAlipayNotify(t, config, url.Values{
		"app_id":       {config.Alipay.AppID},
		"out_trade_no": {"order-a"},
		"trade_no":     {"trade-a"},
		"trade_status": {"TRADE_SUCCESS"},
		"total_amount": {"1.00"},
		"sign_type":    {"RSA2"},
	})
	values.Set("total_amount", "2.00")
	if _, err := (PaymentService{Config: config}).ParseAndVerifyAlipayNotification(values); err == nil {
		t.Fatal("expected invalid signature error")
	}
}

func TestPaymentServiceRejectsAmountMismatch(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	config := testPaymentConfig(t)
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	seedTrafficAccount(t, ctx, s, now, 100)

	service := PaymentService{Store: s, Config: config}
	resp, err := service.CreateAlipayOrder(ctx, PaymentOrderRequest{
		AccountID:             "account-a",
		PackageType:           types.SaaSTrafficPurchasePackageOneTime,
		HighSpeedTrafficBytes: 200,
		AmountCents:           9900,
		Subject:               "100 GB high-speed traffic",
		Body:                  "Cloink traffic package",
		CreatedBy:             "user-a",
	})
	if err != nil {
		t.Fatal(err)
	}

	values := signedAlipayNotify(t, config, url.Values{
		"app_id":       {config.Alipay.AppID},
		"out_trade_no": {resp.Order.ID},
		"trade_no":     {"20260618220000000001"},
		"trade_status": {"TRADE_SUCCESS"},
		"total_amount": {"88.00"},
		"sign_type":    {"RSA2"},
	})
	notification, err := service.ParseAndVerifyAlipayNotification(values)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyAlipayNotification(ctx, *notification); err == nil {
		t.Fatal("expected amount mismatch to fail")
	}
}

func testPaymentConfig(t *testing.T) nbconfig.SaaSPaymentConfig {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	privateKeyFile := dir + "/app-private.pem"
	publicCertFile := dir + "/alipay-public.pem"
	if err := os.WriteFile(privateKeyFile, pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}), 0600); err != nil {
		t.Fatal(err)
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Alipay Test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}, &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Alipay Test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicCertFile, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	}), 0600); err != nil {
		t.Fatal(err)
	}
	return nbconfig.SaaSPaymentConfig{
		Provider: types.SaaSPaymentProviderAlipay,
		Alipay: nbconfig.SaaSAlipayConfig{
			AppID:                "2021000000000000",
			AppPrivateKeyFile:    privateKeyFile,
			AlipayPublicCertFile: publicCertFile,
			NotifyURL:            "https://admin.cloink.4w.ink/api/saas/payments/alipay/notify",
			ReturnURL:            "https://admin.cloink.4w.ink/payments/return",
		},
	}
}

func signedAlipayNotify(t *testing.T, config nbconfig.SaaSPaymentConfig, values url.Values) url.Values {
	t.Helper()
	privateKey, err := loadRSAPrivateKey(config.Alipay.AppPrivateKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := signAlipayParams(values, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	values.Set("sign", signature)
	return values
}
