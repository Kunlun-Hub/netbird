package server

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/permissions"
	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
)

func TestGetAccountEntitlementsReturnsBasicSnapshot(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	permissionsManager := permissions.NewMockManager(ctrl)
	permissionsManager.EXPECT().
		ValidateUserPermissions(gomock.Any(), "account-a", "user-a", modules.Accounts, operations.Read).
		Return(true, nil)

	manager := &DefaultAccountManager{
		permissionsManager:  permissionsManager,
		entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider()),
	}

	snapshot, err := manager.GetAccountEntitlements(context.Background(), "account-a", "user-a")
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.Equal(t, "account-a", snapshot.AccountID)
	assert.Equal(t, entitlements.PlanBasic, snapshot.Plan)
	assert.False(t, snapshot.Features[entitlements.FeatureBranding])
	assert.True(t, snapshot.Features[entitlements.FeatureLocalAuth])
	assert.Equal(t, 3, snapshot.Limits[entitlements.LimitUsers])
	assert.Equal(t, 10, snapshot.Limits[entitlements.LimitPeers])
}

func TestGetAccountEntitlementsRequiresAccountReadPermission(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	permissionsManager := permissions.NewMockManager(ctrl)
	permissionsManager.EXPECT().
		ValidateUserPermissions(gomock.Any(), "account-a", "user-a", modules.Accounts, operations.Read).
		Return(false, nil)

	manager := &DefaultAccountManager{
		permissionsManager:  permissionsManager,
		entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider()),
	}

	snapshot, err := manager.GetAccountEntitlements(context.Background(), "account-a", "user-a")
	require.Error(t, err)
	assert.Nil(t, snapshot)
}
