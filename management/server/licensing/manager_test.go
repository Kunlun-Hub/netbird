package licensing

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
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
	manager := NewManager(t.TempDir(), WithSecret("test-secret"))

	machineID, err := manager.MachineID(context.Background(), "https://cloink.4w.ink/")
	require.NoError(t, err)
	assert.NotEqual(t, "cloink.4w.ink", machineID)
	assert.True(t, strings.HasPrefix(machineID, "U2FsdGVkX1"))

	plaintext, err := DecryptLicenseString(machineID, "test-secret")
	require.NoError(t, err)
	assert.Equal(t, "server_url=cloink.4w.ink,key=test-secret", plaintext)

	again, err := manager.MachineID(context.Background(), "https://cloink.4w.ink/")
	require.NoError(t, err)
	assert.Equal(t, machineID, again)
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
	machinePayload, err := DecryptLicenseString(state.MachineID, "test-secret")
	require.NoError(t, err)
	assert.Equal(t, "server_url=cloink.4w.ink,key=test-secret", machinePayload)
	assert.Equal(t, "cloink.4w.ink", state.ServerURL)
	assert.Equal(t, StatusActive, state.Status)
	assert.Equal(t, entitlements.PlanPro, state.Plan)
	assert.Equal(t, []LicenseType{LicenseTypeYear}, state.LicenseTypes)
	assert.NotEmpty(t, state.LicenseKeyMasked)
	require.NotNil(t, state.UpdatedAt)
	assert.Equal(t, now, *state.UpdatedAt)

	storedState, err := manager.GetState(context.Background(), "cloink.4w.ink")
	require.NoError(t, err)
	assert.Equal(t, state.MachineID, storedState.MachineID)
	assert.Equal(t, StatusActive, storedState.Status)
	assert.Equal(t, entitlements.PlanPro, storedState.Plan)
}

func TestEncryptMachinePayloadUsesStableOpenSSLSaltedFormat(t *testing.T) {
	payload := BuildMachinePayload("https://cloink.4w.ink/", "test-secret")
	first, err := EncryptMachinePayload(payload, "test-secret")
	require.NoError(t, err)
	second, err := EncryptMachinePayload(payload, "test-secret")
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.True(t, strings.HasPrefix(first, "U2FsdGVkX1"))

	plaintext, err := DecryptLicenseString(first, "test-secret")
	require.NoError(t, err)
	assert.Equal(t, "server_url=cloink.4w.ink,key=test-secret", plaintext)
}

func TestManagerAcceptsOpenSSLSaltedAESLicense(t *testing.T) {
	secret := "lA8fsCkh1s7e2JEruZCr0JNChQIfpuDbr6avPSbWgasz2cseGjNcZ225BAuCy4m2CDz8jMSHaQxHSWBXxfo1viFDDZRTDJqQ82oFfietnjhEYpuG1DPslfIpyFLiSvse"
	key := "U2FsdGVkX1+86EPyCJ/iuGmzd9JLs4IcdAVm/KsSU70gF8i/TCrkgPlfBmAPHRJqQ9BQWQLZGl6Oa15cglLaJoYWHp1k0ABJ2E6/7JWmQtkREuVz7uP1FjeLpRcnQRifDjgNHteVpCqk2n7UCWqTTx0+2PG9AAPqqgdR08o0bFO7RwLATOdt9CBrDhCaiH0NsWjoLUSskB6JgkBNSzBth13edoY7xC/3oziJcOtX0qV/fah75WuSZmRcDCQNeYvxBMb9XKd1aygaZVVBLrGFxwwIVBMu3R6VUUrlGn7gOzUwCEZRVUbRB30n+G+5zP6VajpTWBiDgesu/at1F5fCkw=="
	manager := NewManager(t.TempDir(), WithSecret(secret))

	state, err := manager.UpdateKey(context.Background(), "cloink.4w.ink", key)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, state.Status)
	assert.Equal(t, entitlements.PlanPro, state.Plan)
	assert.Equal(t, []LicenseType{LicenseTypeEnterprise}, state.LicenseTypes)
	assert.Equal(t, "xxx公司", state.Name)
}

func TestEncryptLicensePayloadUsesOpenSSLSaltedFormat(t *testing.T) {
	key, err := EncryptLicensePayload("server_url=cloink.4w.ink,license=enterprise,key=test-secret,start_time=2020/1/1,end_time=2099/12/31,name=xxx公司;", "test-secret")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(key, "U2FsdGVkX1"))

	plaintext, err := DecryptLicenseString(key, "test-secret")
	require.NoError(t, err)
	assert.Contains(t, plaintext, "license=enterprise")
	assert.Contains(t, plaintext, "name=xxx公司")
}

func TestManagerParsesLicenseName(t *testing.T) {
	companyName := "xxx公司"
	tests := []struct {
		name       string
		fieldValue string
	}{
		{
			name:       "plain UTF-8",
			fieldValue: companyName,
		},
		{
			name:       "base64 UTF-8",
			fieldValue: base64.StdEncoding.EncodeToString([]byte(companyName)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(t.TempDir(), WithSecret("test-secret"))
			payload := BuildLicensePayloadWithName(
				"cloink.4w.ink",
				[]LicenseType{LicenseTypeYear},
				"test-secret",
				"2020/1/1",
				"2099/12/31",
				tt.fieldValue,
			)
			key, err := EncryptLicensePayload(payload, "test-secret")
			require.NoError(t, err)

			state, err := manager.UpdateKey(context.Background(), "cloink.4w.ink", key)
			require.NoError(t, err)
			assert.Equal(t, companyName, state.Name)
		})
	}
}

func TestManagerTreatsEmptyLicenseListAsEnterprise(t *testing.T) {
	secret := "test-secret"
	manager := NewManager(t.TempDir(), WithSecret(secret))
	payload := "server_url=cloink.4w.ink,license=[],key=test-secret,start_time=2020/1/1,end_time=2099/12/31,name=xxx公司;"
	key, err := EncryptLicensePayload(payload, secret)
	require.NoError(t, err)

	state, err := manager.UpdateKey(context.Background(), "cloink.4w.ink", key)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, state.Status)
	assert.Equal(t, entitlements.PlanPro, state.Plan)
	assert.Equal(t, []LicenseType{LicenseTypeEnterprise}, state.LicenseTypes)
	assert.Equal(t, "xxx公司", state.Name)
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
