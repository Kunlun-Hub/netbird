package server

import (
	"context"
	"fmt"

	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

func (am *DefaultAccountManager) requireUserLimitForCreate(ctx context.Context, accountID string) error {
	users, err := am.Store.GetAccountUsers(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return fmt.Errorf("get account users for entitlement check: %w", err)
	}
	if err := am.requireSaaSLimit(ctx, am.Store, accountID, entitlements.LimitUsers, len(users)+1); err != nil {
		return err
	}

	if am.entitlementsChecker == nil {
		return nil
	}

	return entitlements.RequireLimit(ctx, am.entitlementsChecker, accountID, entitlements.LimitUsers, len(users)+1)
}

func (am *DefaultAccountManager) requireUserInviteLimitForCreate(ctx context.Context, accountID string, userCount int) error {
	invites, err := am.Store.GetAccountUserInvites(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return fmt.Errorf("get account user invites for entitlement check: %w", err)
	}
	projected := userCount + len(invites) + 1
	if err := am.requireSaaSLimit(ctx, am.Store, accountID, entitlements.LimitUsers, projected); err != nil {
		return err
	}

	if am.entitlementsChecker == nil {
		return nil
	}

	return entitlements.RequireLimit(ctx, am.entitlementsChecker, accountID, entitlements.LimitUsers, projected)
}

func (am *DefaultAccountManager) requireUserLimitForCreateTx(ctx context.Context, transaction store.Store, accountID string) error {
	users, err := transaction.GetAccountUsers(ctx, store.LockingStrengthUpdate, accountID)
	if err != nil {
		return fmt.Errorf("get account users for entitlement check: %w", err)
	}
	if err := am.requireSaaSLimit(ctx, transaction, accountID, entitlements.LimitUsers, len(users)+1); err != nil {
		return err
	}

	if am.entitlementsChecker == nil {
		return nil
	}

	return entitlements.RequireLimit(ctx, am.entitlementsChecker, accountID, entitlements.LimitUsers, len(users)+1)
}

func (am *DefaultAccountManager) requirePeerLimitForCreateTx(ctx context.Context, transaction store.Store, accountID string) error {
	peers, err := transaction.GetAccountPeers(ctx, store.LockingStrengthUpdate, accountID, "", "")
	if err != nil {
		return fmt.Errorf("get account peers for entitlement check: %w", err)
	}
	if err := am.requireSaaSLimit(ctx, transaction, accountID, entitlements.LimitPeers, len(peers)+1); err != nil {
		return err
	}

	if am.entitlementsChecker == nil {
		return nil
	}

	return entitlements.RequireLimit(ctx, am.entitlementsChecker, accountID, entitlements.LimitPeers, len(peers)+1)
}

func (am *DefaultAccountManager) requireSaaSLimit(ctx context.Context, s store.Store, accountID string, limit entitlements.Limit, current int) error {
	if s == nil {
		return nil
	}
	subscription, err := s.GetSaaSSubscription(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		if statusErr, ok := status.FromError(err); ok && statusErr.Type() == status.NotFound {
			return nil
		}
		return err
	}

	allowed, ok := saasLimitValue(subscription, limit)
	if !ok || allowed == entitlements.Unlimited || current <= allowed {
		return nil
	}
	return status.ErrorfWithDetails(
		status.PermissionDenied,
		"limit_exceeded",
		map[string]interface{}{
			"limit":         string(limit),
			"current":       current,
			"allowed":       allowed,
			"required_plan": subscription.Plan,
		},
		"limit_exceeded: limit %s allows %d, current %d on %s plan",
		limit,
		allowed,
		current,
		subscription.Plan,
	)
}

func saasLimitValue(subscription *types.SaaSSubscription, limit entitlements.Limit) (int, bool) {
	if subscription == nil {
		return 0, false
	}
	switch limit {
	case entitlements.LimitUsers:
		return subscription.UsersLimit, true
	case entitlements.LimitPeers:
		return subscription.PeersLimit, true
	case entitlements.LimitSelfHostedRelays:
		return subscription.RelaysLimit, true
	default:
		return 0, false
	}
}
