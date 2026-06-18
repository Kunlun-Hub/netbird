package saas

import (
	"context"

	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

type OrganizationStatusGuard struct {
	Store store.Store
}

func (g OrganizationStatusGuard) RequireActive(ctx context.Context, accountID string) error {
	if g.Store == nil || accountID == "" {
		return nil
	}

	org, err := g.Store.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		if statusErr, ok := status.FromError(err); ok && statusErr.Type() == status.NotFound {
			return nil
		}
		return err
	}

	switch org.Status {
	case "", types.SaaSOrganizationStatusActive:
		return nil
	case types.SaaSOrganizationStatusSuspended:
		return status.Errorf(status.PermissionDenied, "organization is suspended")
	case types.SaaSOrganizationStatusDeleted:
		return status.Errorf(status.PermissionDenied, "organization is deleted")
	default:
		return status.Errorf(status.PermissionDenied, "organization is not active")
	}
}
