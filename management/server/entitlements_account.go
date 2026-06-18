package server

import (
	"context"
	"fmt"

	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/shared/management/status"
)

func (am *DefaultAccountManager) GetAccountEntitlements(ctx context.Context, accountID, userID string) (*entitlements.Entitlements, error) {
	allowed, ctx, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Accounts, operations.Read)
	if err != nil {
		return nil, status.NewPermissionValidationError(err)
	}
	if !allowed {
		return nil, status.NewPermissionDeniedError()
	}

	snapshot, err := entitlements.Snapshot(ctx, am.entitlementsChecker, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account entitlements: %w", err)
	}
	if err := am.applySaaSEntitlementOverrides(ctx, accountID, &snapshot); err != nil {
		return nil, err
	}
	usage, err := am.accountEntitlementUsage(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account entitlement usage: %w", err)
	}
	snapshot.Usage = usage
	return &snapshot, nil
}

func (am *DefaultAccountManager) applySaaSEntitlementOverrides(ctx context.Context, accountID string, snapshot *entitlements.Entitlements) error {
	if am.Store == nil || snapshot == nil {
		return nil
	}
	subscription, err := am.Store.GetSaaSSubscription(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		if statusErr, ok := status.FromError(err); ok && statusErr.Type() == status.NotFound {
			return nil
		}
		return err
	}
	if snapshot.Limits == nil {
		snapshot.Limits = map[entitlements.Limit]int{}
	}
	if subscription.Plan != "" {
		snapshot.Plan = entitlements.Plan(subscription.Plan)
	}
	snapshot.Limits[entitlements.LimitUsers] = subscription.UsersLimit
	snapshot.Limits[entitlements.LimitPeers] = subscription.PeersLimit
	snapshot.Limits[entitlements.LimitSelfHostedRelays] = subscription.RelaysLimit
	return nil
}
