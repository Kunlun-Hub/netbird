package saas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/activity"
	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/http/middleware"
	"github.com/netbirdio/netbird/management/server/idp"
	saasmanager "github.com/netbirdio/netbird/management/server/saas"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/shared/management/http/util"
	"github.com/netbirdio/netbird/shared/management/status"
)

var signupRateLimiter = middleware.NewAPIRateLimiter(&middleware.RateLimiterConfig{
	RequestsPerMinute: 5,
	Burst:             3,
	CleanupInterval:   10 * time.Minute,
	LimiterTTL:        30 * time.Minute,
})

type passwordUserCreator interface {
	CreateUserWithPassword(ctx context.Context, email, password, name string) (*idp.UserData, error)
	UpdateUserAppMetadata(ctx context.Context, userID string, appMetadata idp.AppMetadata) error
	DeleteUser(ctx context.Context, userID string) error
}

type handler struct {
	store      store.Store
	config     nbconfig.SaaSConfig
	idpManager passwordUserCreator
	audit      saasmanager.AuditRecorder
}

type signupRequest struct {
	Email            string `json:"email"`
	Password         string `json:"password"`
	Name             string `json:"name"`
	OrganizationName string `json:"organization_name"`
}

type signupResponse struct {
	AccountID          string `json:"account_id"`
	OrganizationDomain string `json:"organization_domain"`
	DashboardURL       string `json:"dashboard_url"`
}

type createAlipayOrderRequest struct {
	PackageType           string `json:"package_type"`
	HighSpeedTrafficBytes int64  `json:"high_speed_traffic_bytes"`
	AmountCents           int64  `json:"amount_cents"`
	Subject               string `json:"subject"`
	Body                  string `json:"body"`
	ReturnURL             string `json:"return_url"`
}

func AddEndpoints(s store.Store, config nbconfig.SaaSConfig, idpManager passwordUserCreator, router *mux.Router, eventStore activity.Store) {
	h := &handler{
		store:      s,
		config:     config,
		idpManager: idpManager,
		audit:      saasmanager.AuditRecorder{Store: eventStore},
	}
	router.Handle("/saas/signup", signupRateLimiter.Middleware(http.HandlerFunc(h.signup))).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/saas/usage", h.getUsage).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/saas/menus", h.getMenus).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/saas/payments/alipay/orders", h.createAlipayOrder).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/saas/payments/alipay/notify", h.alipayNotify).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/saas/payments/orders/{orderId}", h.getPaymentOrder).Methods(http.MethodGet, http.MethodOptions)
}

func (h *handler) signup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		util.WriteErrorResponse("wrong HTTP method", http.StatusMethodNotAllowed, w)
		return
	}
	if h.idpManager == nil {
		util.WriteError(r.Context(), status.Errorf(status.PreconditionFailed, "embedded IdP is required for SaaS signup"), w)
		return
	}

	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}

	userData, err := h.idpManager.CreateUserWithPassword(r.Context(), req.Email, req.Password, req.Name)
	if err != nil {
		log.WithContext(r.Context()).Warnf("failed to create SaaS signup user for email %s: %v", req.Email, err)
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "failed to create signup user"), w)
		return
	}

	provisioner := saasmanager.Provisioner{
		Store:  h.store,
		Config: h.config,
	}
	result, err := provisioner.ProvisionOrganization(r.Context(), userData.ID, saasmanager.SignupRequest{
		Email:            req.Email,
		Password:         req.Password,
		Name:             req.Name,
		OrganizationName: req.OrganizationName,
	})
	if err != nil {
		h.rollbackIDPUser(r.Context(), userData.ID)
		util.WriteError(r.Context(), err, w)
		return
	}

	if err := h.idpManager.UpdateUserAppMetadata(r.Context(), userData.ID, idp.AppMetadata{WTAccountID: result.AccountID}); err != nil {
		h.rollbackProvisioning(r.Context(), result.AccountID, userData.ID)
		log.WithContext(r.Context()).Warnf("failed to bind SaaS signup user %s to organization %s: %v", userData.ID, result.AccountID, err)
		util.WriteError(r.Context(), status.Errorf(status.Internal, "failed to bind signup user to organization"), w)
		return
	}

	h.audit.Record(r.Context(), userData.ID, result.AccountID, result.AccountID, activity.SaaSOrganizationRegistered, map[string]any{
		"email":               req.Email,
		"organization_domain": result.OrganizationDomain,
		"dashboard_url":       result.DashboardURL,
	})

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(signupResponse{
		AccountID:          result.AccountID,
		OrganizationDomain: result.OrganizationDomain,
		DashboardURL:       result.DashboardURL,
	}); err != nil {
		log.WithContext(r.Context()).Warnf("failed to write SaaS signup response: %v", err)
	}
}

func (h *handler) getUsage(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "user is not authenticated"), w)
		return
	}
	resp, err := (saasmanager.TrafficService{Store: h.store}).Usage(r.Context(), userAuth.AccountId, time.Now().UTC())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) getMenus(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "user is not authenticated"), w)
		return
	}
	items, err := h.store.GetSaaSOrgMenuVisibility(r.Context(), store.LockingStrengthNone, userAuth.AccountId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	menus := make(map[string]bool, len(items))
	for _, item := range items {
		menus[item.MenuKey] = item.Visible
	}
	util.WriteJSONObject(r.Context(), w, menus)
}

func (h *handler) createAlipayOrder(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "user is not authenticated"), w)
		return
	}
	var req createAlipayOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("invalid request body", http.StatusBadRequest, w)
		return
	}
	resp, err := (saasmanager.PaymentService{Store: h.store, Config: h.config.Payment, Audit: h.audit}).CreateAlipayOrder(r.Context(), saasmanager.PaymentOrderRequest{
		AccountID:             userAuth.AccountId,
		PackageType:           req.PackageType,
		HighSpeedTrafficBytes: req.HighSpeedTrafficBytes,
		AmountCents:           req.AmountCents,
		Subject:               req.Subject,
		Body:                  req.Body,
		ReturnURL:             req.ReturnURL,
		CreatedBy:             userAuth.UserId,
	})
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) getPaymentOrder(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "user is not authenticated"), w)
		return
	}
	orderID := mux.Vars(r)["orderId"]
	resp, err := (saasmanager.PaymentService{Store: h.store, Config: h.config.Payment}).GetPaymentOrder(r.Context(), userAuth.AccountId, orderID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

func (h *handler) alipayNotify(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		util.WriteErrorResponse("invalid form body", http.StatusBadRequest, w)
		return
	}
	paymentService := saasmanager.PaymentService{Store: h.store, Config: h.config.Payment, Audit: h.audit}
	notification, err := paymentService.ParseAndVerifyAlipayNotification(url.Values(r.PostForm))
	if err != nil {
		h.audit.Record(r.Context(), activity.SystemInitiator, "", "", activity.SaaSPaymentFailed, map[string]any{
			"order_id":     r.PostForm.Get("out_trade_no"),
			"trade_status": r.PostForm.Get("trade_status"),
			"reason":       err.Error(),
		})
		util.WriteError(r.Context(), err, w)
		return
	}
	if _, err := paymentService.ApplyAlipayNotification(r.Context(), *notification); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("success")); err != nil {
		log.WithContext(r.Context()).Warnf("failed to write Alipay notify response: %v", err)
	}
}

func (h *handler) rollbackProvisioning(ctx context.Context, accountID, userID string) {
	if h.store != nil && accountID != "" {
		if err := h.store.DeleteSaaSDataByAccountID(ctx, accountID); err != nil {
			log.WithContext(ctx).Warnf("failed to rollback SaaS metadata for account %s: %v", accountID, err)
		}
		account, err := h.store.GetAccount(ctx, accountID)
		if err == nil {
			if err := h.store.DeleteAccount(ctx, account); err != nil {
				log.WithContext(ctx).Warnf("failed to rollback SaaS signup account %s: %v", accountID, err)
			}
		}
	}
	h.rollbackIDPUser(ctx, userID)
}

func (h *handler) rollbackIDPUser(ctx context.Context, userID string) {
	if h.idpManager == nil || userID == "" {
		return
	}
	if err := h.idpManager.DeleteUser(ctx, userID); err != nil {
		log.WithContext(ctx).Warnf("failed to rollback SaaS signup IdP user %s: %v", userID, err)
	}
}
