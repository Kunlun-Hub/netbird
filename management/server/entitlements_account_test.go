package server

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/management/internals/modules/reverseproxy/domain"
	rpservice "github.com/netbirdio/netbird/management/internals/modules/reverseproxy/service"
	"github.com/netbirdio/netbird/management/server/entitlements"
	nbpeer "github.com/netbirdio/netbird/management/server/peer"
	"github.com/netbirdio/netbird/management/server/permissions"
	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestGetAccountEntitlementsReturnsBasicSnapshot(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	permissionsManager := permissions.NewMockManager(ctrl)
	permissionsManager.EXPECT().
		ValidateUserPermissions(gomock.Any(), "account-a", "user-a", modules.Accounts, operations.Read).
		Return(true, context.Background(), nil)

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
		Return(false, context.Background(), nil)

	manager := &DefaultAccountManager{
		permissionsManager:  permissionsManager,
		entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider()),
	}

	snapshot, err := manager.GetAccountEntitlements(context.Background(), "account-a", "user-a")
	require.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestAccountEntitlementUsageCountsResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountUsers(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return([]*types.User{{Id: "user-a"}, {Id: "user-b"}}, nil)
	mockStore.EXPECT().
		GetAccountPeers(gomock.Any(), store.LockingStrengthNone, "account-a", "", "").
		Return([]*nbpeer.Peer{{ID: "peer-a"}, {ID: "peer-b"}, {ID: "peer-c"}}, nil)
	mockStore.EXPECT().
		GetAccountSettings(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return(&types.Settings{
			Extra: &types.ExtraSettings{
				RegisteredRelays: map[string]types.RegisteredRelay{
					"relay-a": {ID: "relay-a", Address: "relay-a.example"},
				},
				RelayPeerPreferences: map[string][]string{
					"peer-a": {"relay-a", "relay-b"},
				},
			},
		}, nil)
	mockStore.EXPECT().
		GetAccountServices(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return([]*rpservice.Service{
			{ID: "service-a", Targets: []*rpservice.Target{{}, {}}},
			{ID: "service-b", Terminated: true, Targets: []*rpservice.Target{{}}},
		}, nil)
	mockStore.EXPECT().
		ListCustomDomains(gomock.Any(), "account-a").
		Return([]*domain.Domain{{ID: "domain-a"}, {ID: "domain-b"}}, nil)

	manager := &DefaultAccountManager{Store: mockStore}

	usage, err := manager.accountEntitlementUsage(context.Background(), "account-a")
	require.NoError(t, err)
	assert.Equal(t, 2, usage[entitlements.LimitUsers])
	assert.Equal(t, 3, usage[entitlements.LimitPeers])
	assert.Equal(t, 2, usage[entitlements.LimitSelfHostedRelays])
	assert.Equal(t, 1, usage[entitlements.LimitReverseProxyServer])
	assert.Equal(t, 2, usage[entitlements.LimitCustomDomains])
	assert.Equal(t, 1, usage[entitlements.LimitCustomRules])
}
