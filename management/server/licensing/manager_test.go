package licensing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/management/server/entitlements"
)

func TestNormalizeServerURL(t *testing.T) {
	assert.Equal(t, "cloink.4w.ink", NormalizeServerURL("https://cloink.4w.ink/"))
	assert.Equal(t, "cloink.4w.ink", NormalizeServerURL("cloink.4w.ink/"))
	assert.Equal(t, "localhost", NormalizeServerURL("http://localhost:3000/settings"))
}

func TestManagerUsesDashboardDomainAsMachineID(t *testing.T) {
	manager := NewManager(t.TempDir())

	machineID, err := manager.MachineID(context.Background(), "https://cloink.4w.ink/")
	require.NoError(t, err)
	assert.Equal(t, "cloink.4w.ink", machineID)
}

func TestManagerAcceptsAESLicenseForCurrentDashboardDomain(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	manager := NewManager(t.TempDir(), WithSecret("test-secret"), WithNow(func() time.Time {
		return now
	}))

	payload := BuildLicensePayload(
		"cloink.4w.ink",
		[]LicenseType{LicenseTypeYear},
		"test-secret",
		"2020/1/1",
		"2099/12/31",
	)
	key, err := EncryptLicensePayload(payload, "test-secret")
	require.NoError(t, err)

	state, err := manager.UpdateKey(context.Background(), "https://cloink.4w.ink/", key)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "cloink.4w.ink", state.MachineID)
	assert.Equal(t, "cloink.4w.ink", state.ServerURL)
	assert.Equal(t, StatusActive, state.Status)
	assert.Equal(t, entitlements.PlanPro, state.Plan)
	assert.Equal(t, []LicenseType{LicenseTypeYear}, state.LicenseTypes)
	assert.NotEmpty(t, state.LicenseKeyMasked)
	require.NotNil(t, state.UpdatedAt)
	assert.Equal(t, now, *state.UpdatedAt)

	storedState, err := manager.GetState(context.Background(), "cloink.4w.ink")
	require.NoError(t, err)
	assert.Equal(t, StatusActive, storedState.Status)
	assert.Equal(t, entitlements.PlanPro, storedState.Plan)
}

func TestManagerRejectsInvalidLicenseWithoutPersistingIt(t *testing.T) {
	manager := NewManager(t.TempDir(), WithSecret("test-secret"))

	state, err := manager.UpdateKey(context.Background(), "cloink.4w.ink", "not-a-license")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidLicenseKey))
	assert.Equal(t, StatusInvalid, state.Status)
	assert.Equal(t, entitlements.PlanBasic, state.Plan)

	storedState, err := manager.GetState(context.Background(), "cloink.4w.ink")
	require.NoError(t, err)
	assert.Equal(t, StatusUnlicensed, storedState.Status)
	assert.Equal(t, entitlements.PlanBasic, storedState.Plan)
}

func TestManagerDetectsURLMismatch(t *testing.T) {
	manager := NewManager(t.TempDir(), WithSecret("test-secret"))
	payload := BuildLicensePayload(
		"licensed.4w.ink",
		[]LicenseType{LicenseTypeEnterprise},
		"test-secret",
		"2020/1/1",
		"2099/12/31",
	)
	key, err := EncryptLicensePayload(payload, "test-secret")
	require.NoError(t, err)

	state, err := manager.UpdateKey(context.Background(), "cloink.4w.ink", key)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidLicenseKey))
	assert.Equal(t, StatusURLMismatch, state.Status)
	assert.Equal(t, "licensed.4w.ink", state.ServerURL)
	assert.Equal(t, entitlements.PlanBasic, state.Plan)
}

func TestManagerDetectsExpiredLicense(t *testing.T) {
	manager := NewManager(t.TempDir(), WithSecret("test-secret"), WithNow(func() time.Time {
		return time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	}))
	payload := BuildLicensePayload(
		"cloink.4w.ink",
		[]LicenseType{LicenseTypeTrial},
		"test-secret",
		"2020/1/1",
		"2021/1/1",
	)
	key, err := EncryptLicensePayload(payload, "test-secret")
	require.NoError(t, err)

	state, err := manager.UpdateKey(context.Background(), "cloink.4w.ink", key)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidLicenseKey))
	assert.Equal(t, StatusExpired, state.Status)
	assert.Equal(t, entitlements.PlanBasic, state.Plan)
}

func TestEntitlementsProviderReturnsPlanFromStoredLicense(t *testing.T) {
	manager := NewManager(t.TempDir(), WithSecret("test-secret"))
	payload := BuildLicensePayload(
		"cloink.4w.ink",
		[]LicenseType{LicenseTypeEnterprise},
		"test-secret",
		"2020/1/1",
		"2099/12/31",
	)
	key, err := EncryptLicensePayload(payload, "test-secret")
	require.NoError(t, err)

	_, err = manager.UpdateKey(context.Background(), "cloink.4w.ink", key)
	require.NoError(t, err)

	provider := NewEntitlementsProvider(manager)
	snapshot, err := provider.GetEntitlements(context.Background(), "account-a")
	require.NoError(t, err)
	assert.Equal(t, "account-a", snapshot.AccountID)
	assert.Equal(t, entitlements.PlanPro, snapshot.Plan)
	assert.True(t, snapshot.Features[entitlements.FeatureBranding])
	assert.Equal(t, entitlements.Unlimited, snapshot.Limits[entitlements.LimitUsers])
}
