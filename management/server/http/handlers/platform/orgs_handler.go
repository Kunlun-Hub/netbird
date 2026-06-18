package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/netbirdio/netbird/management/server/activity"
	saasmanager "github.com/netbirdio/netbird/management/server/saas"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/http/util"
	"github.com/netbirdio/netbird/shared/management/status"
)

type handler struct {
	store store.Store
	audit saasmanager.AuditRecorder
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

type menusRequest struct {
	Menus map[string]bool `json:"menus"`
}

type trafficPurchaseRequest struct {
	PackageType           string `json:"package_type,omitempty"`
	HighSpeedTrafficBytes int64  `json:"high_speed_traffic_bytes"`
	CreatedBy             string `json:"created_by,omitempty"`
}

func AddEndpoints(s store.Store, router *mux.Router, eventStore activity.Store) {
	h := &handler{store: s, audit: saasmanager.AuditRecorder{Store: eventStore}}
	router.HandleFunc("/platform/orgs", h.listOrganizations).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}", h.getOrganization).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/suspend", h.suspendOrganization).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/resume", h.resumeOrganization).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/limits", h.updateLimits).Methods(http.MethodPatch, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/menus", h.updateMenus).Methods(http.MethodPatch, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/usage", h.getUsage).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/traffic-purchases", h.createTrafficPurchase).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/platform/orgs/{accountId}/payment-orders", h.listPaymentOrders).Methods(http.MethodGet, http.MethodOptions)
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
