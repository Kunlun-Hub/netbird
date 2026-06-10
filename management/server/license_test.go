package server

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/licensing"
	"github.com/netbirdio/netbird/management/server/permissions"
	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
)

func TestGetAccountLicenseReturnsMachineAndBasicStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	permissionsManager := permissions.NewMockManager(ctrl)
	permissionsManager.EXPECT().
		ValidateUserPermissions(gomock.Any(), "account-a", "user-a", modules.Accounts, operations.Read).
		Return(true, context.Background(), nil)

	manager := &DefaultAccountManager{
		permissionsManager: permissionsManager,
		licenseManager:     licensing.NewManager(t.TempDir(), licensing.WithSecret("test-secret")),
	}

	state, err := manager.GetAccountLicense(context.Background(), "account-a", "user-a", "cloink.4w.ink")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.NotEmpty(t, state.MachineID)
	assert.Equal(t, licensing.StatusUnlicensed, state.Status)
	assert.Equal(t, entitlements.PlanBasic, state.Plan)
}

func TestUpdateAccountLicenseActivatesPro(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	permissionsManager := permissions.NewMockManager(ctrl)
	permissionsManager.EXPECT().
		ValidateUserPermissions(gomock.Any(), "account-a", "user-a", modules.Settings, operations.Update).
		Return(true, context.Background(), nil)

	licenseManager := licensing.NewManager(t.TempDir(), licensing.WithSecret("test-secret"))
	payload := licensing.BuildLicensePayload(
		"cloink.4w.ink",
		[]licensing.LicenseType{licensing.LicenseTypeYear},
		"test-secret",
		"2020/1/1",
		"2099/12/31",
	)
	licenseKey, err := licensing.EncryptLicensePayload(payload, "test-secret")
	require.NoError(t, err)

	manager := &DefaultAccountManager{
		permissionsManager: permissionsManager,
		licenseManager:     licenseManager,
	}

	state, err := manager.UpdateAccountLicense(context.Background(), "account-a", "user-a", "cloink.4w.ink", licenseKey)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, licensing.StatusActive, state.Status)
	assert.Equal(t, entitlements.PlanPro, state.Plan)
}

func TestUpdateAccountLicenseRequiresAccountUpdatePermission(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	permissionsManager := permissions.NewMockManager(ctrl)
	permissionsManager.EXPECT().
		ValidateUserPermissions(gomock.Any(), "account-a", "user-a", modules.Settings, operations.Update).
		Return(false, context.Background(), nil)

	manager := &DefaultAccountManager{
		permissionsManager: permissionsManager,
		licenseManager:     licensing.NewManager(t.TempDir(), licensing.WithSecret("test-secret")),
	}

	state, err := manager.UpdateAccountLicense(context.Background(), "account-a", "user-a", "cloink.4w.ink", "bad-key")
	require.Error(t, err)
	assert.Nil(t, state)
}
