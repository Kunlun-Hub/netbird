package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/activity"
	emailmanager "github.com/netbirdio/netbird/management/server/email"
	saasmanager "github.com/netbirdio/netbird/management/server/saas"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	nbdomain "github.com/netbirdio/netbird/shared/management/domain"
	"github.com/netbirdio/netbird/shared/management/http/util"
	"github.com/netbirdio/netbird/shared/management/status"
)

type handler struct {
	store  store.Store
	audit  saasmanager.AuditRecorder
	email  emailNotifier
	config nbconfig.SaaSConfig
}

type emailNotifier interface {
	Notify(ctx context.Context, accountID string, kind types.EmailTemplateKind, data emailmanager.TemplateData) error
}

type organization struct {
	AccountID             string `json:"account_id"`
	Slug                  string `json:"slug"`
	Domain                string `json:"domain"`
	DisplayName           string `json:"display_name"`
	Status                string `json:"status"`
	CreatedAt             string `json:"created_at"`
	UserCount             int    `json:"user_count,omitempty"`
	PeerCount             int    `json:"peer_count,omitempty"`
	Plan                  string `json:"plan,omitempty"`
	SubscriptionStatus    string `json:"subscription_status,omitempty"`
	TrialEndsAt           string `json:"trial_ends_at,omitempty"`
	ExpiresAt             string `json:"expires_at,omitempty"`
	UsersLimit            int    `json:"users_limit,omitempty"`
	PeersLimit            int    `json:"peers_limit,omitempty"`
	RelaysLimit           int    `json:"relays_limit,omitempty"`
	HighSpeedTrafficBytes int64  `json:"high_speed_traffic_bytes,omitempty"`
	StandardRateLimitMbps int    `json:"standard_rate_limit_mbps,omitempty"`
	TotalRateLimitMbps    int    `json:"total_rate_limit_mbps,omitempty"`
	RelayOnlyAccounting   bool   `json:"relay_only_accounting,omitempty"`
	FairShareEnabled      bool   `json:"fair_share_enabled,omitempty"`
	OwnerEmail            string `json:"owner_email,omitempty"`
}

type limitsRequest struct {
	UsersLimit            *int   `json:"users_limit,omitempty"`
	PeersLimit            *int   `json:"peers_limit,omitempty"`
	RelaysLimit           *int   `json:"relays_limit,omitempty"`
	HighSpeedTrafficBytes *int64 `json:"high_speed_traffic_bytes,omitempty"`
	StandardRateLimitMbps *int   `json:"standard_rate_limit_mbps,omitempty"`
	TotalRateLimitMbps    *int   `json:"total_rate_limit_mbps,omitempty"`
}

type subscriptionRequest struct {
	Plan        *string    `json:"plan,omitempty"`
	Status      *string    `json:"status,omitempty"`
	TrialEndsAt *time.Time `json:"trial_ends_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type renewSubscriptionRequest struct {
	Plan           string `json:"plan,omitempty"`
	PeriodDays     int    `json:"period_days,omitempty"`
	HighSpeedGB    int    `json:"high_speed_gb,omitempty"`
	OperatorID     string `json:"operator_id,omitempty"`
	Reason         string `json:"reason,omitempty"`
	PaymentOrderID string `json:"payment_order_id,omitempty"`
}

type domainRequest struct {
	Domain string `json:"domain"`
}

type menusRequest struct {
	Menus map[string]bool `json:"menus"`
}

type trafficPurchaseRequest struct {
	PackageType           string `json:"package_type,omitempty"`
	HighSpeedTrafficBytes int64  `json:"high_speed_traffic_bytes"`
	CreatedBy             string `json:"created_by,omitempty"`
}

type refundRequest struct {
	OrderID       string         `json:"order_id"`
	RefundTradeNo string         `json:"refund_trade_no,omitempty"`
	AmountCents   int64          `json:"amount_cents,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	OperatorID    string         `json:"operator_id,omitempty"`
	Payload       map[string]any `json:"payload,omitempty"`
}

type closePaymentOrderRequest struct {
	Reason     string `json:"reason,omitempty"`
	OperatorID string `json:"operator_id,omitempty"`
}

type reconciliationRequest struct {
	OrderID           string         `json:"order_id,omitempty"`
	ProviderTradeNo   string         `json:"provider_trade_no,omitempty"`
	ActualAmountCents int64          `json:"actual_amount_cents,omitempty"`
	Currency          string         `json:"currency,omitempty"`
	Status            string         `json:"status,omitempty"`
	Reason            string         `json:"reason,omitempty"`
	OperatorID        string         `json:"operator_id,omitempty"`
	RawPayload        map[string]any `json:"raw_payload,omitempty"`
}

type invoiceRequest struct {
	BillID       string `json:"bill_id,omitempty"`
	InvoiceTitle string `json:"invoice_title"`
	TaxID        string `json:"tax_id,omitempty"`
	Email        string `json:"email,omitempty"`
	AmountCents  int64  `json:"amount_cents,omitempty"`
	Currency     string `json:"currency,omitempty"`
	OperatorID   string `json:"operator_id,omitempty"`
}

type invoiceUpdateRequest struct {
	Status     string `json:"status,omitempty"`
	InvoiceNo  string `json:"invoice_no,omitempty"`
	InvoiceURL string `json:"invoice_url,omitempty"`
	Reason     string `json:"reason,omitempty"`
	OperatorID string `json:"operator_id,omitempty"`
}

type offlinePaymentRequest struct {
	Provider        string         `json:"provider,omitempty"`
	ProviderTradeNo string         `json:"provider_trade_no,omitempty"`
	AmountCents     int64          `json:"amount_cents"`
	Currency        string         `json:"currency,omitempty"`
	Status          string         `json:"status,omitempty"`
	Plan            string         `json:"plan,omitempty"`
	PeriodDays      int            `json:"period_days,omitempty"`
	HighSpeedGB     int            `json:"high_speed_gb,omitempty"`
	PaymentOrderID  string         `json:"payment_order_id,omitempty"`
	BillID          string         `json:"bill_id,omitempty"`
	Note            string         `json:"note,omitempty"`
	OperatorID      string         `json:"operator_id,omitempty"`
	Payload         map[string]any `json:"payload,omitempty"`
}

type autoRenewalAttemptRequest struct {
	Provider       string         `json:"provider,omitempty"`
	AmountCents    int64          `json:"amount_cents,omitempty"`
	Currency       string         `json:"currency,omitempty"`
	Status         string         `json:"status,omitempty"`
	PaymentOrderID string         `json:"payment_order_id,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	NextRetryAt    *time.Time     `json:"next_retry_at,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
}

func AddEndpoints(s store.Store, config nbconfig.SaaSConfig, router *mux.Router, eventStore activity.Store, emailSvc emailNotifier) {
	config.ApplyDefaults()
	h := &handler{store: s, audit: saasmanager.AuditRecorder{Store: eventStore}, email: emailSvc, config: config}
	router.HandleFunc("/platform/orgs", h.listOrganizations).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/subscriptions/lifecycle", h.processSubscriptionLifecycle).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}", h.getOrganization).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/suspend", h.suspendOrganization).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/resume", h.resumeOrganization).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/limits", h.updateLimits).Methods(http.MethodPatch, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/subscription", h.updateSubscription).Methods(http.MethodPatch, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/subscription/renew", h.renewSubscription).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/domain", h.updateDomain).Methods(http.MethodPatch, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/menus", h.updateMenus).Methods(http.MethodPatch, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/usage", h.getUsage).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/traffic-purchases", h.createTrafficPurchase).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/payment-orders", h.listPaymentOrders).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/payment-orders/{orderId}/close", h.closePaymentOrder).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/bills", h.listBills).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/refunds", h.createRefund).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/refunds", h.listRefunds).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/reconciliation-records", h.createReconciliationRecord).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/reconciliation-records", h.listReconciliationRecords).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/invoices", h.createInvoiceRequest).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/invoices", h.listInvoiceRequests).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/invoices/{invoiceId}", h.updateInvoiceRequest).Methods(http.MethodPatch, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/offline-payments", h.createOfflinePayment).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/offline-payments", h.listOfflinePayments).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/auto-renewal-attempts", h.createAutoRenewalAttempt).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/auto-renewal-attempts", h.listAutoRenewalAttempts).Methods(http.MethodGet, http.MethodOptions)
}

func (h *handler) listOrganizations(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.store.ListSaaSOrganizations(r.Context(), store.LockingStrengthNone)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	resp := make([]organization, 0, len(orgs))
	for _, org := range orgs {
		details, err := h.organizationDetails(r.Context(), org.AccountID)
		if err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}
		resp = append(resp, *details)
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) getOrganization(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	resp, err := h.organizationDetails(r.Context(), accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) suspendOrganization(w http.ResponseWriter, r *http.Request) {
	h.updateOrganizationStatus(w, r, types.SaaSOrganizationStatusSuspended)
}

func (h *handler) resumeOrganization(w http.ResponseWriter, r *http.Request) {
	h.updateOrganizationStatus(w, r, types.SaaSOrganizationStatusActive)
}

func (h *handler) updateOrganizationStatus(w http.ResponseWriter, r *http.Request, statusValue string) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	org, err := h.store.GetSaaSOrganizationByAccountID(r.Context(), store.LockingStrengthUpdate, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	org.Status = statusValue
	if err := h.store.SaveSaaSOrganization(r.Context(), org); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	event := activity.SaaSOrganizationSuspended
	if statusValue == types.SaaSOrganizationStatusActive {
		event = activity.SaaSOrganizationResumed
	}
	h.audit.Record(r.Context(), activity.SystemInitiator, accountID, accountID, event, map[string]any{
		"status": statusValue,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) updateLimits(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	var req limitsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}

	subscription, err := h.store.GetSaaSSubscription(r.Context(), store.LockingStrengthUpdate, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	policy, err := h.store.GetSaaSBandwidthPolicy(r.Context(), store.LockingStrengthUpdate, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	if req.UsersLimit != nil {
		subscription.UsersLimit = *req.UsersLimit
	}
	if req.PeersLimit != nil {
		subscription.PeersLimit = *req.PeersLimit
	}
	if req.RelaysLimit != nil {
		subscription.RelaysLimit = *req.RelaysLimit
	}
	if req.HighSpeedTrafficBytes != nil {
		subscription.HighSpeedTrafficBytes = *req.HighSpeedTrafficBytes
	}
	if req.StandardRateLimitMbps != nil {
		subscription.StandardRateLimitMbps = *req.StandardRateLimitMbps
		policy.StandardRateLimitMbps = *req.StandardRateLimitMbps
	}
	if req.TotalRateLimitMbps != nil {
		subscription.TotalRateLimitMbps = *req.TotalRateLimitMbps
		policy.TotalRateLimitMbps = *req.TotalRateLimitMbps
		policy.HighSpeedRateLimitMbps = *req.TotalRateLimitMbps
	}

	now := time.Now().UTC()
	subscription.UpdatedAt = now
	policy.UpdatedAt = now

	if err := h.store.SaveSaaSSubscription(r.Context(), subscription); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if err := h.store.SaveSaaSBandwidthPolicy(r.Context(), policy); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	h.audit.Record(r.Context(), activity.SystemInitiator, accountID, accountID, activity.SaaSLimitsUpdated, map[string]any{
		"users_limit":              subscription.UsersLimit,
		"peers_limit":              subscription.PeersLimit,
		"relays_limit":             subscription.RelaysLimit,
		"high_speed_traffic_bytes": subscription.HighSpeedTrafficBytes,
		"standard_rate_limit_mbps": subscription.StandardRateLimitMbps,
		"total_rate_limit_mbps":    subscription.TotalRateLimitMbps,
	})
	h.audit.Record(r.Context(), activity.SystemInitiator, accountID, accountID, activity.SaaSBandwidthUpdated, map[string]any{
		"standard_rate_limit_mbps":   policy.StandardRateLimitMbps,
		"total_rate_limit_mbps":      policy.TotalRateLimitMbps,
		"high_speed_rate_limit_mbps": policy.HighSpeedRateLimitMbps,
		"relay_only_accounting":      policy.RelayOnlyAccounting,
		"fair_share_enabled":         policy.FairShareEnabled,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) updateSubscription(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	var req subscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}

	subscription, err := h.store.GetSaaSSubscription(r.Context(), store.LockingStrengthUpdate, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	if req.Plan != nil {
		subscription.Plan = strings.TrimSpace(*req.Plan)
	}
	if req.Status != nil {
		statusValue := strings.TrimSpace(*req.Status)
		if !isAllowedSubscriptionStatus(statusValue) {
			util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid subscription status"), w)
			return
		}
		subscription.Status = statusValue
	}
	if req.TrialEndsAt != nil {
		trialEndsAt := req.TrialEndsAt.UTC()
		subscription.TrialEndsAt = &trialEndsAt
	}
	if req.ExpiresAt != nil {
		expiresAt := req.ExpiresAt.UTC()
		subscription.ExpiresAt = &expiresAt
	}

	subscription.UpdatedAt = time.Now().UTC()
	if err := h.store.SaveSaaSSubscription(r.Context(), subscription); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	h.audit.Record(r.Context(), activity.SystemInitiator, accountID, accountID, activity.SaaSLimitsUpdated, map[string]any{
		"plan":          subscription.Plan,
		"status":        subscription.Status,
		"trial_ends_at": formatTimePtr(subscription.TrialEndsAt),
		"expires_at":    formatTimePtr(subscription.ExpiresAt),
	})
	if h.email != nil {
		saasmanager.SubscriptionNotifier{Store: h.store, Email: h.email}.NotifyChanged(r.Context(), accountID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) renewSubscription(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	var req renewSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	resp, err := (saasmanager.SubscriptionLifecycleService{
		Store:    h.store,
		Config:   h.config,
		Audit:    h.audit,
		Notifier: saasmanager.SubscriptionNotifier{Store: h.store, Email: h.email},
	}).Renew(r.Context(), saasmanager.RenewSubscriptionRequest{
		AccountID:      accountID,
		Plan:           strings.TrimSpace(req.Plan),
		PeriodDays:     req.PeriodDays,
		HighSpeedGB:    req.HighSpeedGB,
		OperatorID:     req.OperatorID,
		Reason:         req.Reason,
		PaymentOrderID: req.PaymentOrderID,
	})
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) processSubscriptionLifecycle(w http.ResponseWriter, r *http.Request) {
	resp, err := (saasmanager.SubscriptionLifecycleService{
		Store:    h.store,
		Config:   h.config,
		Audit:    h.audit,
		Notifier: saasmanager.SubscriptionNotifier{Store: h.store, Email: h.email},
	}).Process(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func isAllowedSubscriptionStatus(statusValue string) bool {
	switch statusValue {
	case types.SaaSSubscriptionStatusActive,
		types.SaaSSubscriptionStatusTrialing,
		types.SaaSSubscriptionStatusPastDue,
		types.SaaSSubscriptionStatusSuspended,
		types.SaaSSubscriptionStatusCanceled:
		return true
	default:
		return false
	}
}

func (h *handler) updateDomain(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	var req domainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}

	org, err := h.store.GetSaaSOrganizationByAccountID(r.Context(), store.LockingStrengthNone, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	domain, err := normalizeOrganizationDomain(req.Domain)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if domain == org.Domain {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	slug := org.Slug
	if candidate := strings.Split(domain, ".")[0]; saasmanager.ValidSlug(candidate) && !saasmanager.IsReservedSlug(candidate) {
		slug = candidate
	}
	if err := h.store.UpdateSaaSOrganizationDomain(r.Context(), accountID, slug, domain); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	h.audit.Record(r.Context(), activity.SystemInitiator, accountID, accountID, activity.SaaSOrganizationDomainUpdated, map[string]any{
		"old_domain": org.Domain,
		"new_domain": domain,
		"old_slug":   org.Slug,
		"new_slug":   slug,
	})
	w.WriteHeader(http.StatusNoContent)
}

func normalizeOrganizationDomain(value string) (string, error) {
	normalized := strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
	if normalized == "" {
		return "", status.Errorf(status.InvalidArgument, "organization domain is required")
	}
	if strings.HasPrefix(normalized, "*.") || !nbdomain.IsValidDomainNoWildcard(normalized) {
		return "", status.Errorf(status.InvalidArgument, "invalid organization domain")
	}
	return normalized, nil
}

func (h *handler) updateMenus(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	if _, err := h.store.GetSaaSOrganizationByAccountID(r.Context(), store.LockingStrengthNone, accountID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	var req menusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	if len(req.Menus) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "menus are required"), w)
		return
	}

	now := time.Now().UTC()
	for menuKey, visible := range req.Menus {
		menuKey = strings.TrimSpace(menuKey)
		if menuKey == "" {
			util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "menu key is required"), w)
			return
		}
		if err := h.store.SaveSaaSOrgMenuVisibility(r.Context(), &types.SaaSOrgMenuVisibility{
			AccountID: accountID,
			MenuKey:   menuKey,
			Visible:   visible,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}
	}
	h.audit.Record(r.Context(), activity.SystemInitiator, accountID, accountID, activity.SaaSMenuVisibilityUpdated, map[string]any{
		"menus": req.Menus,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) getUsage(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	resp, err := (saasmanager.TrafficService{Store: h.store}).Usage(r.Context(), accountID, time.Now().UTC())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) createTrafficPurchase(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	var req trafficPurchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	purchase, err := (saasmanager.TrafficService{Store: h.store}).ApplyTrafficPurchase(r.Context(), saasmanager.TrafficPurchaseRequest{
		AccountID:             accountID,
		PackageType:           req.PackageType,
		HighSpeedTrafficBytes: req.HighSpeedTrafficBytes,
		CreatedBy:             req.CreatedBy,
	})
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	h.audit.Record(r.Context(), activity.SystemInitiator, purchase.ID, accountID, activity.SaaSTrafficPurchaseGranted, map[string]any{
		"package_type":             purchase.PackageType,
		"high_speed_traffic_bytes": purchase.HighSpeedTrafficBytes,
		"created_by":               req.CreatedBy,
	})
	util.WriteJSONObject(r.Context(), w, purchase)
}

func (h *handler) listPaymentOrders(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	if _, err := h.store.GetSaaSOrganizationByAccountID(r.Context(), store.LockingStrengthNone, accountID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	orders, err := h.store.ListSaaSPaymentOrders(r.Context(), store.LockingStrengthNone, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, orders)
}

func (h *handler) closePaymentOrder(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	orderID := mux.Vars(r)["orderId"]
	if accountID == "" || orderID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id and order id are required"), w)
		return
	}
	var req closePaymentOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	resp, err := (saasmanager.PaymentService{Store: h.store, Audit: h.audit}).ClosePaymentOrder(r.Context(), saasmanager.ClosePaymentOrderRequest{
		AccountID:  accountID,
		OrderID:    orderID,
		Reason:     req.Reason,
		OperatorID: req.OperatorID,
	})
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) listBills(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	resp, err := (saasmanager.BillingService{Store: h.store}).ListBills(r.Context(), accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) createRefund(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	var req refundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	resp, err := (saasmanager.PaymentService{Store: h.store, Audit: h.audit}).RefundPaymentOrder(r.Context(), saasmanager.RefundRequest{
		AccountID:       accountID,
		OrderID:         req.OrderID,
		RefundTradeNo:   req.RefundTradeNo,
		AmountCents:     req.AmountCents,
		Reason:          req.Reason,
		OperatorID:      req.OperatorID,
		ProviderPayload: req.Payload,
	})
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) listRefunds(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	if _, err := h.store.GetSaaSOrganizationByAccountID(r.Context(), store.LockingStrengthNone, accountID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	refunds, err := h.store.ListSaaSPaymentRefunds(r.Context(), store.LockingStrengthNone, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, refunds)
}

func (h *handler) createReconciliationRecord(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	var req reconciliationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	resp, err := (saasmanager.PaymentService{Store: h.store, Audit: h.audit}).RecordReconciliation(r.Context(), saasmanager.ReconciliationRequest{
		AccountID:         accountID,
		OrderID:           req.OrderID,
		Provider:          types.SaaSPaymentProviderAlipay,
		ProviderTradeNo:   req.ProviderTradeNo,
		ActualAmountCents: req.ActualAmountCents,
		Currency:          req.Currency,
		Status:            req.Status,
		Reason:            req.Reason,
		OperatorID:        req.OperatorID,
		RawPayload:        req.RawPayload,
	})
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) listReconciliationRecords(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	if _, err := h.store.GetSaaSOrganizationByAccountID(r.Context(), store.LockingStrengthNone, accountID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	records, err := h.store.ListSaaSReconciliationRecords(r.Context(), store.LockingStrengthNone, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, records)
}

func (h *handler) createInvoiceRequest(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	var req invoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	invoice, err := (saasmanager.FinanceService{Store: h.store, Audit: h.audit}).RequestInvoice(r.Context(), saasmanager.InvoiceRequestInput{
		AccountID:    accountID,
		BillID:       req.BillID,
		InvoiceTitle: req.InvoiceTitle,
		TaxID:        req.TaxID,
		Email:        req.Email,
		AmountCents:  req.AmountCents,
		Currency:     req.Currency,
		OperatorID:   req.OperatorID,
	})
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, invoice)
}

func (h *handler) updateInvoiceRequest(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	invoiceID := mux.Vars(r)["invoiceId"]
	if accountID == "" || invoiceID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id and invoice id are required"), w)
		return
	}
	var req invoiceUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	invoice, err := (saasmanager.FinanceService{Store: h.store, Audit: h.audit}).UpdateInvoice(r.Context(), saasmanager.InvoiceUpdateInput{
		AccountID:  accountID,
		InvoiceID:  invoiceID,
		Status:     req.Status,
		InvoiceNo:  req.InvoiceNo,
		InvoiceURL: req.InvoiceURL,
		Reason:     req.Reason,
		OperatorID: req.OperatorID,
	})
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, invoice)
}

func (h *handler) listInvoiceRequests(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	invoices, err := h.store.ListSaaSInvoiceRequests(r.Context(), store.LockingStrengthNone, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, invoices)
}

func (h *handler) createOfflinePayment(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	var req offlinePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	record, err := (saasmanager.FinanceService{
		Store:    h.store,
		Audit:    h.audit,
		Notifier: saasmanager.SubscriptionNotifier{Store: h.store, Email: h.email},
	}).RecordOfflinePayment(r.Context(), saasmanager.OfflinePaymentInput{
		AccountID:       accountID,
		Provider:        req.Provider,
		ProviderTradeNo: req.ProviderTradeNo,
		AmountCents:     req.AmountCents,
		Currency:        req.Currency,
		Status:          req.Status,
		Plan:            req.Plan,
		PeriodDays:      req.PeriodDays,
		HighSpeedGB:     req.HighSpeedGB,
		PaymentOrderID:  req.PaymentOrderID,
		BillID:          req.BillID,
		Note:            req.Note,
		OperatorID:      req.OperatorID,
		Payload:         req.Payload,
	})
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, record)
}

func (h *handler) listOfflinePayments(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	records, err := h.store.ListSaaSOfflinePaymentRecords(r.Context(), store.LockingStrengthNone, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, records)
}

func (h *handler) createAutoRenewalAttempt(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	var req autoRenewalAttemptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	attempt, err := (saasmanager.FinanceService{
		Store:    h.store,
		Audit:    h.audit,
		Notifier: saasmanager.SubscriptionNotifier{Store: h.store, Email: h.email},
	}).RecordAutoRenewalAttempt(r.Context(), saasmanager.AutoRenewalAttemptInput{
		AccountID:      accountID,
		Provider:       req.Provider,
		AmountCents:    req.AmountCents,
		Currency:       req.Currency,
		Status:         req.Status,
		PaymentOrderID: req.PaymentOrderID,
		Reason:         req.Reason,
		NextRetryAt:    req.NextRetryAt,
		Payload:        req.Payload,
	})
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, attempt)
}

func (h *handler) listAutoRenewalAttempts(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "account id is required"), w)
		return
	}
	attempts, err := h.store.ListSaaSAutoRenewalAttempts(r.Context(), store.LockingStrengthNone, accountID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, attempts)
}

func (h *handler) organizationDetails(ctx context.Context, accountID string) (*organization, error) {
	org, err := h.store.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}
	subscription, err := h.store.GetSaaSSubscription(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}
	policy, err := h.store.GetSaaSBandwidthPolicy(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}
	users, err := h.store.GetAccountUsers(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}
	peers, err := h.store.GetAccountPeers(ctx, store.LockingStrengthNone, accountID, "", "")
	if err != nil {
		return nil, err
	}
	owner, err := h.store.GetAccountOwner(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}

	return &organization{
		AccountID:             org.AccountID,
		Slug:                  org.Slug,
		Domain:                org.Domain,
		DisplayName:           org.DisplayName,
		Status:                org.Status,
		CreatedAt:             org.CreatedAt.Format(time.RFC3339),
		UserCount:             len(users),
		PeerCount:             len(peers),
		Plan:                  subscription.Plan,
		SubscriptionStatus:    subscription.Status,
		TrialEndsAt:           formatTimePtr(subscription.TrialEndsAt),
		ExpiresAt:             formatTimePtr(subscription.ExpiresAt),
		UsersLimit:            subscription.UsersLimit,
		PeersLimit:            subscription.PeersLimit,
		RelaysLimit:           subscription.RelaysLimit,
		HighSpeedTrafficBytes: subscription.HighSpeedTrafficBytes,
		StandardRateLimitMbps: subscription.StandardRateLimitMbps,
		TotalRateLimitMbps:    subscription.TotalRateLimitMbps,
		RelayOnlyAccounting:   policy.RelayOnlyAccounting,
		FairShareEnabled:      policy.FairShareEnabled,
		OwnerEmail:            owner.Email,
	}, nil
}

func formatTimePtr(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
