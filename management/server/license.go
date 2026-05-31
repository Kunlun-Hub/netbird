package server

import (
	"context"
	"errors"

	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/licensing"
	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
	"github.com/netbirdio/netbird/shared/management/status"
)

func (am *DefaultAccountManager) GetAccountLicense(ctx context.Context, accountID, userID, serverURL string) (*licensing.State, error) {
	allowed, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Accounts, operations.Read)
	if err != nil {
		return nil, status.NewPermissionValidationError(err)
	}
	if !allowed {
		return nil, status.NewPermissionDeniedError()
	}

	if am.licenseManager == nil {
		return nil, status.Errorf(status.Internal, "license manager is not available")
	}
	state, err := am.licenseManager.GetState(ctx, serverURL)
	if err != nil {
		return nil, err
	}
	usage, err := am.accountEntitlementUsage(ctx, accountID)
	if err != nil {
		return nil, err
	}
	state.Usage = usage
	return state, nil
}

func (am *DefaultAccountManager) UpdateAccountLicense(ctx context.Context, accountID, userID, serverURL, licenseKey string) (*licensing.State, error) {
	allowed, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Settings, operations.Update)
	if err != nil {
		return nil, status.NewPermissionValidationError(err)
	}
	if !allowed {
		return nil, status.NewPermissionDeniedError()
	}

	if am.licenseManager == nil {
		return nil, status.Errorf(status.Internal, "license manager is not available")
	}

	state, err := am.licenseManager.UpdateKey(ctx, serverURL, licenseKey)
	if errors.Is(err, licensing.ErrInvalidLicenseKey) {
		return nil, status.ErrorfWithDetails(
			status.InvalidArgument,
			"invalid_license_key",
			map[string]interface{}{
				"required_plan": string(entitlements.PlanPro),
			},
			"invalid_license_key: %s",
			state.Message,
		)
	}
	if err != nil {
		return nil, err
	}
	usage, err := am.accountEntitlementUsage(ctx, accountID)
	if err != nil {
		return nil, err
	}
	state.Usage = usage
	return state, nil
}
