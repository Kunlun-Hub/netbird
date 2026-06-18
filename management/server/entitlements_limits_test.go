package server

import (
	"context"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/netbirdio/netbird/management/server/entitlements"
	nbpeer "github.com/netbirdio/netbird/management/server/peer"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

func TestRequireUserLimitForCreateBasicDeniesFourthUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountUsers(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return([]*types.User{{}, {}, {}}, nil)
	mockStore.EXPECT().
		GetSaaSSubscription(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return(nil, status.Errorf(status.NotFound, "saas subscription not found"))

	manager := basicEntitlementsAccountManager()
	manager.Store = mockStore

	err := manager.requireUserLimitForCreate(context.Background(), "account-a")
	assertLimitDenied(t, err, entitlements.LimitUsers)
}

func TestRequireUserInviteLimitForCreateBasicDeniesFourthUserOrInvite(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountUserInvites(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return([]*types.UserInviteRecord{{}}, nil)
	mockStore.EXPECT().
		GetSaaSSubscription(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return(nil, status.Errorf(status.NotFound, "saas subscription not found"))

	manager := basicEntitlementsAccountManager()
	manager.Store = mockStore

	err := manager.requireUserInviteLimitForCreate(context.Background(), "account-a", 2)
	assertLimitDenied(t, err, entitlements.LimitUsers)
}

func TestRequirePeerLimitForCreateBasicDeniesEleventhPeer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	peers := make([]*nbpeer.Peer, 10)
	for i := range peers {
		peers[i] = &nbpeer.Peer{}
	}

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountPeers(gomock.Any(), store.LockingStrengthUpdate, "account-a", "", "").
		Return(peers, nil)
	mockStore.EXPECT().
		GetSaaSSubscription(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return(nil, status.Errorf(status.NotFound, "saas subscription not found"))

	manager := basicEntitlementsAccountManager()

	err := manager.requirePeerLimitForCreateTx(context.Background(), mockStore, "account-a")
	assertLimitDenied(t, err, entitlements.LimitPeers)
}

func TestRequireUserLimitForCreateUsesSaaSSubscription(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountUsers(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return([]*types.User{{}, {}}, nil)
	mockStore.EXPECT().
		GetSaaSSubscription(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return(&types.SaaSSubscription{Plan: "free", UsersLimit: 2}, nil)

	manager := &DefaultAccountManager{Store: mockStore}

	err := manager.requireUserLimitForCreate(context.Background(), "account-a")
	assertLimitDenied(t, err, entitlements.LimitUsers)
}

func TestRequireUserInviteLimitForCreateUsesSaaSSubscription(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountUserInvites(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return([]*types.UserInviteRecord{{}}, nil)
	mockStore.EXPECT().
		GetSaaSSubscription(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return(&types.SaaSSubscription{Plan: "free", UsersLimit: 2}, nil)

	manager := &DefaultAccountManager{Store: mockStore}

	err := manager.requireUserInviteLimitForCreate(context.Background(), "account-a", 1)
	assertLimitDenied(t, err, entitlements.LimitUsers)
}

func TestRequirePeerLimitForCreateUsesSaaSSubscription(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountPeers(gomock.Any(), store.LockingStrengthUpdate, "account-a", "", "").
		Return([]*nbpeer.Peer{{}, {}}, nil)
	mockStore.EXPECT().
		GetSaaSSubscription(gomock.Any(), store.LockingStrengthNone, "account-a").
		Return(&types.SaaSSubscription{Plan: "free", PeersLimit: 2}, nil)

	manager := &DefaultAccountManager{Store: mockStore}

	err := manager.requirePeerLimitForCreateTx(context.Background(), mockStore, "account-a")
	assertLimitDenied(t, err, entitlements.LimitPeers)
}

func assertLimitDenied(t *testing.T, err error, limit entitlements.Limit) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected limit %s to be denied", limit)
	}
	if !strings.Contains(err.Error(), "limit_exceeded") || !strings.Contains(err.Error(), string(limit)) {
		t.Fatalf("error = %q, want limit_exceeded for %s", err, limit)
	}
}
