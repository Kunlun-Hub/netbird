package saas

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/activity"
	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/idp"
	saasmanager "github.com/netbirdio/netbird/management/server/saas"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	nbauth "github.com/netbirdio/netbird/shared/auth"
	"github.com/netbirdio/netbird/shared/management/status"
)

type mockPasswordUserCreator struct {
	createFn func(ctx context.Context, email, password, name string) (*idp.UserData, error)
	updateFn func(ctx context.Context, userID string, appMetadata idp.AppMetadata) error
	deleteFn func(ctx context.Context, userID string) error

	deletedUserID string
}

func (m *mockPasswordUserCreator) CreateUserWithPassword(ctx context.Context, email, password, name string) (*idp.UserData, error) {
	if m.createFn != nil {
		return m.createFn(ctx, email, password, name)
	}
	return &idp.UserData{ID: "user-a", Email: email, Name: name}, nil
}

func (m *mockPasswordUserCreator) UpdateUserAppMetadata(ctx context.Context, userID string, appMetadata idp.AppMetadata) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, userID, appMetadata)
	}
	return nil
}

func (m *mockPasswordUserCreator) DeleteUser(ctx context.Context, userID string) error {
	m.deletedUserID = userID
	if m.deleteFn != nil {
		return m.deleteFn(ctx, userID)
	}
	return nil
}

func TestSignupCreatesOrganization(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	router := mux.NewRouter()
	idpManager := &mockPasswordUserCreator{}
	eventStore := &activity.InMemoryEventStore{}
	AddEndpoints(s, testSaaSConfig(), idpManager, router, eventStore)

	body := []byte(`{"email":"alice@example.com","password":"Password1!","name":"Alice","organization_name":"Alice Team"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/saas/signup", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	var resp signupResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccountID == "" || resp.OrganizationDomain == "" || resp.DashboardURL == "" {
		t.Fatalf("incomplete response: %+v", resp)
	}
	user, err := s.GetUserByUserID(ctx, store.LockingStrengthNone, "user-a")
	if err != nil {
		t.Fatal(err)
	}
	if user.AccountID != resp.AccountID {
		t.Fatalf("user account = %s, want %s", user.AccountID, resp.AccountID)
	}
	events, err := eventStore.Get(ctx, resp.AccountID, 0, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Activity != activity.SaaSOrganizationRegistered {
		t.Fatalf("unexpected signup audit events: %+v", events)
	}
}

func TestSignupRateLimited(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	router := mux.NewRouter()
	idpManager := &mockPasswordUserCreator{}
	AddEndpoints(s, testSaaSConfig(), idpManager, router, &activity.InMemoryEventStore{})

	for i := 0; i < 4; i++ {
		rec := httptest.NewRecorder()
		body := []byte(`{"email":"rate` + strconv.Itoa(i) + `@example.com","password":"Password1!","name":"Rate","organization_name":"Rate Team"}`)
		req := httptest.NewRequest(http.MethodPost, "/saas/signup", bytes.NewReader(body))
		req.RemoteAddr = "192.0.2.10:1234"
		router.ServeHTTP(rec, req)
		if i < 3 && rec.Code == http.StatusTooManyRequests {
			t.Fatalf("unexpected rate limit on request %d: %s", i+1, rec.Body.String())
		}
		if i == 3 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("expected rate limit on request 4, got %d: %s", rec.Code, rec.Body.String())
		}
	}
}

func TestSignupRollsBackIDPUserOnProvisioningError(t *testing.T) {
	router := mux.NewRouter()
	idpManager := &mockPasswordUserCreator{}
	AddEndpoints(nil, testSaaSConfig(), idpManager, router, &activity.InMemoryEventStore{})

	body := []byte(`{"email":"alice@example.com","password":"Password1!","name":"Alice","organization_name":"Alice Team"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/saas/signup", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusCreated {
		t.Fatal("expected signup failure")
	}
	if idpManager.deletedUserID != "user-a" {
		t.Fatalf("expected rollback of user-a, got %q", idpManager.deletedUserID)
	}
}

func TestSignupRollsBackProvisioningOnMetadataError(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	router := mux.NewRouter()
	idpManager := &mockPasswordUserCreator{
		updateFn: func(ctx context.Context, userID string, appMetadata idp.AppMetadata) error {
			return status.Errorf(status.Internal, "metadata error")
		},
	}
	AddEndpoints(s, testSaaSConfig(), idpManager, router, &activity.InMemoryEventStore{})

	body := []byte(`{"email":"alice@example.com","password":"Password1!","name":"Alice","organization_name":"Alice Team"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/saas/signup", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusCreated {
		t.Fatal("expected signup failure")
	}
	if idpManager.deletedUserID != "user-a" {
		t.Fatalf("expected rollback of user-a, got %q", idpManager.deletedUserID)
	}
	if len(s.GetAllAccounts(ctx)) != 0 {
		t.Fatalf("expected provisioned account rollback, got %d accounts", len(s.GetAllAccounts(ctx)))
	}
	organizations, err := s.ListSaaSOrganizations(ctx, store.LockingStrengthNone)
	if err != nil {
		t.Fatal(err)
	}
	if len(organizations) != 0 {
		t.Fatalf("expected SaaS metadata rollback, got %+v", organizations)
	}
}

func TestSignupRejectsDuplicateEmail(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	router := mux.NewRouter()
	idpManager := &mockPasswordUserCreator{
		createFn: func(ctx context.Context, email, password, name string) (*idp.UserData, error) {
			return nil, status.Errorf(status.AlreadyExists, "user already exists")
		},
	}
	AddEndpoints(s, testSaaSConfig(), idpManager, router, &activity.InMemoryEventStore{})

	body := []byte(`{"email":"dup@example.com","password":"Password1!","name":"Dup","organization_name":"Dup Team"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/saas/signup", bytes.NewReader(body))
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusCreated {
		t.Fatal("expected duplicate signup to fail")
	}
}

func TestGetUsageReturnsCurrentOrganizationTraffic(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Now().UTC()
	if err := s.SaveSaaSOrganization(ctx, &types.SaaSOrganization{
		AccountID:   "account-a",
		Slug:        "accounta",
		Domain:      "accounta.cloink.4w.ink",
		DisplayName: "Account A",
		Status:      types.SaaSOrganizationStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSSubscription(ctx, &types.SaaSSubscription{
		AccountID:             "account-a",
		Plan:                  "free",
		Status:                types.SaaSSubscriptionStatusTrialing,
		HighSpeedTrafficBytes: 1000,
		CreatedAt:             now,
		UpdatedAt:             now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSBandwidthPolicy(ctx, &types.SaaSBandwidthPolicy{
		AccountID:             "account-a",
		StandardRateLimitMbps: 10,
		TotalRateLimitMbps:    50,
		UpdatedAt:             now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSaaSTrafficLedger(ctx, &types.SaaSTrafficLedger{
		ID:         "ledger-a",
		AccountID:  "account-a",
		PeriodKey:  now.Format("2006-01"),
		Source:     types.SaaSTrafficSourceRelay,
		Direction:  "both",
		Bytes:      200,
		Tier:       types.SaaSTrafficTierHighSpeed,
		EventID:    "event-a",
		RecordedAt: now,
		CreatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}

	router := mux.NewRouter()
	AddEndpoints(s, testSaaSConfig(), &mockPasswordUserCreator{}, router, &activity.InMemoryEventStore{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/saas/usage", nil)
	req = nbcontext.SetUserAuthInRequest(req, nbauth.UserAuth{AccountId: "account-a", UserId: "user-a"})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	var resp saasmanager.TrafficUsage
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccountID != "account-a" || resp.HighSpeedUsedBytes != 200 || resp.HighSpeedRemainingBytes != 800 {
		t.Fatalf("unexpected usage response: %+v", resp)
	}
}

func TestGetMenusReturnsCurrentOrganizationVisibility(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Now().UTC()
	seedPaymentAccount(t, ctx, s, now)
	if err := s.SaveSaaSOrgMenuVisibility(ctx, &types.SaaSOrgMenuVisibility{
		AccountID: "account-a",
		MenuKey:   "dns",
		Visible:   false,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	router := mux.NewRouter()
	AddEndpoints(s, testSaaSConfig(), &mockPasswordUserCreator{}, router, &activity.InMemoryEventStore{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/saas/menus", nil)
	req = nbcontext.SetUserAuthInRequest(req, nbauth.UserAuth{AccountId: "account-a", UserId: "user-a"})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if visible, ok := resp["dns"]; !ok || visible {
		t.Fatalf("unexpected menus: %+v", resp)
	}
}

func TestAlipayPaymentEndpointsCreateQueryAndApplyNotify(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Now().UTC()
	seedPaymentAccount(t, ctx, s, now)
	config := testSaaSConfig()
	config.Payment = saasmanagerTestPaymentConfig(t)

	router := mux.NewRouter()
	AddEndpoints(s, config, &mockPasswordUserCreator{}, router, &activity.InMemoryEventStore{})

	body := []byte(`{"package_type":"one_time","high_speed_traffic_bytes":512,"amount_cents":9900,"subject":"100 GB traffic","body":"Cloink traffic package"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/saas/payments/alipay/orders", bytes.NewReader(body))
	req = nbcontext.SetUserAuthInRequest(req, nbauth.UserAuth{AccountId: "account-a", UserId: "user-a"})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected create status %d: %s", rec.Code, rec.Body.String())
	}
	var created saasmanager.PaymentOrderResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Order.ID == "" || created.PayURL == "" || created.Purchase.Status != types.SaaSTrafficPurchaseStatusPending {
		t.Fatalf("unexpected created payment order: %+v", created)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/saas/payments/orders/"+created.Order.ID, nil)
	req = nbcontext.SetUserAuthInRequest(req, nbauth.UserAuth{AccountId: "account-a", UserId: "user-a"})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected query status %d: %s", rec.Code, rec.Body.String())
	}

	values := saasmanagerSignedAlipayNotify(t, config.Payment, url.Values{
		"app_id":       {config.Payment.Alipay.AppID},
		"out_trade_no": {created.Order.ID},
		"trade_no":     {"20260618220000000002"},
		"trade_status": {"TRADE_SUCCESS"},
		"total_amount": {"99.00"},
		"sign_type":    {"RSA2"},
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/saas/payments/alipay/notify", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "success" {
		t.Fatalf("unexpected notify response %d: %s", rec.Code, rec.Body.String())
	}
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.HighSpeedTrafficBytes != 612 {
		t.Fatalf("expected traffic package to apply, got %d", subscription.HighSpeedTrafficBytes)
	}
}

func TestListBills(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	seedPaymentAccount(t, ctx, s, now)
	bill := &types.SaaSBill{
		ID:             "bill-a",
		AccountID:      "account-a",
		PeriodKey:      "2026-06",
		Status:         types.SaaSBillStatusPaid,
		SubtotalCents:  9900,
		TotalCents:     9900,
		Currency:       "CNY",
		PaymentOrderID: "order-a",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.SaveSaaSBill(ctx, bill); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSBillItem(ctx, &types.SaaSBillItem{
		ID:                    "bill-item-a",
		BillID:                bill.ID,
		AccountID:             bill.AccountID,
		ItemType:              types.SaaSBillItemTypeTrafficPackage,
		Description:           "traffic package",
		Quantity:              1,
		UnitAmountCents:       9900,
		AmountCents:           9900,
		Currency:              "CNY",
		HighSpeedTrafficBytes: 100 << 30,
		CreatedAt:             now,
	}); err != nil {
		t.Fatal(err)
	}

	router := mux.NewRouter()
	AddEndpoints(s, testSaaSConfig(), nil, router, &activity.InMemoryEventStore{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/saas/bills", nil)
	req = nbcontext.SetUserAuthInRequest(req, nbauth.UserAuth{UserId: "user-a", AccountId: "account-a"})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp []saasmanager.BillResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) != 1 || resp[0].Bill.ID != bill.ID || len(resp[0].Items) != 1 {
		t.Fatalf("unexpected bills: %+v", resp)
	}
}

func testSaaSConfig() nbconfig.SaaSConfig {
	return nbconfig.SaaSConfig{
		Enabled:                      true,
		PublicSignupEnabled:          true,
		OrganizationDomainSuffix:     "cloink.4w.ink",
		DefaultPlan:                  "free",
		DefaultUsersLimit:            3,
		DefaultPeersLimit:            10,
		DefaultRelaysLimit:           1,
		DefaultHighSpeedTrafficGB:    100,
		DefaultTotalRateLimitMbps:    50,
		DefaultStandardRateLimitMbps: 10,
		ForceRelayForTrafficBilling:  true,
	}
}

func seedPaymentAccount(t *testing.T, ctx context.Context, s store.Store, now time.Time) {
	t.Helper()
	if err := s.SaveSaaSOrganization(ctx, &types.SaaSOrganization{
		AccountID:   "account-a",
		Slug:        "accounta",
		Domain:      "accounta.cloink.4w.ink",
		DisplayName: "Account A",
		Status:      types.SaaSOrganizationStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSSubscription(ctx, &types.SaaSSubscription{
		AccountID:             "account-a",
		Plan:                  "free",
		Status:                types.SaaSSubscriptionStatusTrialing,
		HighSpeedTrafficBytes: 100,
		CreatedAt:             now,
		UpdatedAt:             now,
	}); err != nil {
		t.Fatal(err)
	}
}

func saasmanagerTestPaymentConfig(t *testing.T) nbconfig.SaaSPaymentConfig {
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
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Alipay Test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
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

func saasmanagerSignedAlipayNotify(t *testing.T, config nbconfig.SaaSPaymentConfig, values url.Values) url.Values {
	t.Helper()
	data, err := os.ReadFile(config.Alipay.AppPrivateKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("invalid test private key")
	}
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(saasmanagerCanonicalAlipayParams(values)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	values.Set("sign", base64.StdEncoding.EncodeToString(signature))
	return values
}

func saasmanagerCanonicalAlipayParams(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key == "sign" || key == "sign_type" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := values.Get(key)
		if value == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "&")
}
