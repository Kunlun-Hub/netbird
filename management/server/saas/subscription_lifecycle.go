package saas

import (
	"context"
	"time"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/activity"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

const (
	defaultSubscriptionGracePeriod = 7 * 24 * time.Hour
	defaultSubscriptionCancelAfter = 30 * 24 * time.Hour
)

type SubscriptionLifecycleService struct {
	Store    store.Store
	Config   nbconfig.SaaSConfig
	Audit    AuditRecorder
	Notifier SubscriptionNotifier
	Now      func() time.Time
}

type SubscriptionLifecycleChange struct {
	AccountID    string `json:"account_id"`
	Previous     string `json:"previous_status"`
	Current      string `json:"current_status"`
	Reason       string `json:"reason"`
	EffectiveAt  string `json:"effective_at"`
	Notification bool   `json:"notification"`
}

type SubscriptionLifecycleResult struct {
	Changes []SubscriptionLifecycleChange `json:"changes"`
}

type RenewSubscriptionRequest struct {
	AccountID      string
	Plan           string
	PeriodDays     int
	HighSpeedGB    int
	OperatorID     string
	Reason         string
	PaymentOrderID string
}

type RenewSubscriptionResponse struct {
	Subscription *types.SaaSSubscription `json:"subscription"`
}

func (s SubscriptionLifecycleService) Process(ctx context.Context) (*SubscriptionLifecycleResult, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	orgs, err := s.Store.ListSaaSOrganizations(ctx, store.LockingStrengthNone)
	if err != nil {
		return nil, err
	}
	result := &SubscriptionLifecycleResult{}
	now := s.now()
	for _, org := range orgs {
		if org.Status != types.SaaSOrganizationStatusActive {
			continue
		}
		subscription, err := s.Store.GetSaaSSubscription(ctx, store.LockingStrengthUpdate, org.AccountID)
		if err != nil {
			return nil, err
		}
		change, changed := s.transitionSubscription(subscription, now)
		if !changed {
			continue
		}
		if err := s.Store.SaveSaaSSubscription(ctx, subscription); err != nil {
			return nil, err
		}
		s.Audit.Record(ctx, activity.SystemInitiator, subscription.AccountID, subscription.AccountID, activity.SaaSSubscriptionLifecycleUpdated, map[string]any{
			"previous_status": change.Previous,
			"current_status":  change.Current,
			"reason":          change.Reason,
			"effective_at":    change.EffectiveAt,
		})
		notified := s.notifyChanged(ctx, subscription.AccountID)
		change.Notification = notified
		result.Changes = append(result.Changes, change)
	}
	return result, nil
}

func (s SubscriptionLifecycleService) Renew(ctx context.Context, req RenewSubscriptionRequest) (*RenewSubscriptionResponse, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if req.AccountID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id is required")
	}
	if req.PeriodDays <= 0 {
		req.PeriodDays = 30
	}
	now := s.now()
	var subscription *types.SaaSSubscription
	err := s.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		locked, err := tx.GetSaaSSubscription(ctx, store.LockingStrengthUpdate, req.AccountID)
		if err != nil {
			return err
		}
		subscription = locked
		if req.Plan != "" {
			subscription.Plan = req.Plan
		}
		subscription.Status = types.SaaSSubscriptionStatusActive
		expiresBase := now
		if subscription.ExpiresAt != nil && subscription.ExpiresAt.After(now) {
			expiresBase = subscription.ExpiresAt.UTC()
		}
		expiresAt := expiresBase.AddDate(0, 0, req.PeriodDays)
		subscription.ExpiresAt = &expiresAt
		if req.HighSpeedGB > 0 {
			subscription.HighSpeedTrafficBytes += int64(req.HighSpeedGB) * 1024 * 1024 * 1024
		}
		subscription.UpdatedAt = now
		return tx.SaveSaaSSubscription(ctx, subscription)
	})
	if err != nil {
		return nil, err
	}
	s.Audit.Record(ctx, req.OperatorID, req.AccountID, req.AccountID, activity.SaaSSubscriptionRenewed, map[string]any{
		"plan":             subscription.Plan,
		"period_days":      req.PeriodDays,
		"expires_at":       formatSubscriptionTime(subscription.ExpiresAt),
		"high_speed_gb":    req.HighSpeedGB,
		"reason":           req.Reason,
		"payment_order_id": req.PaymentOrderID,
	})
	s.notifyChanged(ctx, req.AccountID)
	return &RenewSubscriptionResponse{Subscription: subscription}, nil
}

func (s SubscriptionLifecycleService) transitionSubscription(subscription *types.SaaSSubscription, now time.Time) (SubscriptionLifecycleChange, bool) {
	previous := subscription.Status
	reason := ""
	switch subscription.Status {
	case types.SaaSSubscriptionStatusTrialing:
		if !isSubscriptionExpired(now, subscription.TrialEndsAt) {
			return SubscriptionLifecycleChange{}, false
		}
		subscription.Status = types.SaaSSubscriptionStatusPastDue
		reason = "trial expired"
	case "", types.SaaSSubscriptionStatusActive:
		if !isSubscriptionExpired(now, subscription.ExpiresAt) {
			return SubscriptionLifecycleChange{}, false
		}
		subscription.Status = types.SaaSSubscriptionStatusPastDue
		reason = "subscription expired"
	case types.SaaSSubscriptionStatusPastDue:
		if subscriptionPastThreshold(now, subscription, s.gracePeriod()) {
			subscription.Status = types.SaaSSubscriptionStatusSuspended
			reason = "grace period elapsed"
			break
		}
		return SubscriptionLifecycleChange{}, false
	case types.SaaSSubscriptionStatusSuspended:
		if subscriptionPastThreshold(now, subscription, s.cancelAfter()) {
			subscription.Status = types.SaaSSubscriptionStatusCanceled
			reason = "cancel period elapsed"
			break
		}
		return SubscriptionLifecycleChange{}, false
	default:
		return SubscriptionLifecycleChange{}, false
	}
	subscription.UpdatedAt = now
	return SubscriptionLifecycleChange{
		AccountID:    subscription.AccountID,
		Previous:     previous,
		Current:      subscription.Status,
		Reason:       reason,
		EffectiveAt:  now.Format(time.RFC3339),
		Notification: false,
	}, true
}

func (s SubscriptionLifecycleService) notifyChanged(ctx context.Context, accountID string) bool {
	notifier := s.Notifier
	if notifier.Store == nil {
		notifier.Store = s.Store
	}
	if notifier.Email == nil {
		return false
	}
	notifier.NotifyChanged(ctx, accountID)
	return true
}

func (s SubscriptionLifecycleService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s SubscriptionLifecycleService) gracePeriod() time.Duration {
	if s.Config.SubscriptionGracePeriodDays <= 0 {
		return defaultSubscriptionGracePeriod
	}
	return time.Duration(s.Config.SubscriptionGracePeriodDays) * 24 * time.Hour
}

func (s SubscriptionLifecycleService) cancelAfter() time.Duration {
	if s.Config.SubscriptionCancelAfterDays <= 0 {
		return defaultSubscriptionCancelAfter
	}
	return time.Duration(s.Config.SubscriptionCancelAfterDays) * 24 * time.Hour
}

func subscriptionPastThreshold(now time.Time, subscription *types.SaaSSubscription, threshold time.Duration) bool {
	base := subscriptionLifecycleBaseTime(subscription)
	if base == nil {
		return false
	}
	return !now.Before(base.UTC().Add(threshold))
}

func subscriptionLifecycleBaseTime(subscription *types.SaaSSubscription) *time.Time {
	base := subscription.ExpiresAt
	if subscription.TrialEndsAt != nil && (base == nil || subscription.TrialEndsAt.After(*base)) {
		base = subscription.TrialEndsAt
	}
	return base
}
