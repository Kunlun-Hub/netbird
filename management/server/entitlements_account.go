package server

import (
	"context"
	"fmt"

	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
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
	usage, err := am.accountEntitlementUsage(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account entitlement usage: %w", err)
	}
	snapshot.Usage = usage
	return &snapshot, nil
}
