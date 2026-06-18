package saas

import (
	"context"

	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

type PlatformAuthorizer struct {
	Store store.Store
}

func (a PlatformAuthorizer) RequireAdmin(ctx context.Context, userID string) (*types.SaaSPlatformAdmin, error) {
	if a.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if userID == "" {
		return nil, status.Errorf(status.Unauthorized, "user is not authenticated")
	}

	admin, err := a.Store.GetSaaSPlatformAdmin(ctx, store.LockingStrengthNone, userID)
	if err != nil {
		return nil, status.Errorf(status.PermissionDenied, "platform admin role required")
	}
	if !admin.Enabled {
		return nil, status.Errorf(status.PermissionDenied, "platform admin is disabled")
	}
	if admin.Role != types.SaaSPlatformRoleAdmin && admin.Role != types.SaaSPlatformRoleReadOnly {
		return nil, status.Errorf(status.PermissionDenied, "platform admin role required")
	}
	return admin, nil
}
