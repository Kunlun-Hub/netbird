package saas

import (
	"context"
	"time"

	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

type SubscriptionStatusGuard struct {
	Store store.Store
	Now   func() time.Time
}

func (g SubscriptionStatusGuard) RequireActive(ctx context.Context, accountID string) error {
	if g.Store == nil || accountID == "" {
		return nil
	}

	subscription, err := g.Store.GetSaaSSubscription(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		if statusErr, ok := status.FromError(err); ok && statusErr.Type() == status.NotFound {
			return nil
		}
		return err
	}

	now := g.now()
	switch subscription.Status {
	case "", types.SaaSSubscriptionStatusActive:
		if !isSubscriptionExpired(now, subscription.ExpiresAt) {
			return nil
		}
		return status.Errorf(status.PermissionDenied, "subscription has expired")
	case types.SaaSSubscriptionStatusTrialing:
		if !isSubscriptionExpired(now, subscription.TrialEndsAt) {
			return nil
		}
		return status.Errorf(status.PermissionDenied, "subscription trial has expired")
	case types.SaaSSubscriptionStatusPastDue:
		return status.Errorf(status.PermissionDenied, "subscription is past due")
	case types.SaaSSubscriptionStatusSuspended:
		return status.Errorf(status.PermissionDenied, "subscription is suspended")
	case types.SaaSSubscriptionStatusCanceled:
		return status.Errorf(status.PermissionDenied, "subscription is canceled")
	default:
		return status.Errorf(status.PermissionDenied, "subscription is not active")
	}
}

func (g SubscriptionStatusGuard) now() time.Time {
	if g.Now != nil {
		return g.Now().UTC()
	}
	return time.Now().UTC()
}

func isSubscriptionExpired(now time.Time, expiresAt *time.Time) bool {
	if expiresAt != nil && !now.Before(expiresAt.UTC()) {
		return true
	}
	return false
}
