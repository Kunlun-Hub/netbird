package saas

import (
	"context"
	"strings"
	"time"

	emailmanager "github.com/netbirdio/netbird/management/server/email"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

type SubscriptionNotifier struct {
	Store store.Store
	Email emailNotifier
}

type emailNotifier interface {
	Notify(ctx context.Context, accountID string, kind types.EmailTemplateKind, data emailmanager.TemplateData) error
}

func (n SubscriptionNotifier) NotifyChanged(ctx context.Context, accountID string) {
	if n.Email == nil || n.Store == nil || strings.TrimSpace(accountID) == "" {
		return
	}
	subscription, err := n.Store.GetSaaSSubscription(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return
	}
	org, err := n.Store.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return
	}
	data := emailmanager.TemplateData{
		"account": map[string]any{
			"name": org.DisplayName,
		},
		"dashboard": map[string]any{
			"url": "https://" + org.Domain,
		},
		"subscription": map[string]any{
			"plan":          subscription.Plan,
			"status":        subscription.Status,
			"status_label":  subscriptionStatusLabel(subscription.Status),
			"trial_ends_at": formatSubscriptionTime(subscription.TrialEndsAt),
			"expires_at":    formatSubscriptionTime(subscription.ExpiresAt),
		},
	}
	_ = n.Email.Notify(ctx, accountID, types.EmailTemplateSubscriptionStatusChanged, data)
}

func subscriptionStatusLabel(statusValue string) string {
	switch statusValue {
	case types.SaaSSubscriptionStatusTrialing:
		return "试用中"
	case types.SaaSSubscriptionStatusPastDue:
		return "欠费"
	case types.SaaSSubscriptionStatusSuspended:
		return "已冻结"
	case types.SaaSSubscriptionStatusCanceled:
		return "已取消"
	default:
		return "生效中"
	}
}

func formatSubscriptionTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}
