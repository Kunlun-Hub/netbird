package server

import (
	"context"
	"fmt"

	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/store"
)

func (am *DefaultAccountManager) requireUserLimitForCreate(ctx context.Context, accountID string) error {
	if am.entitlementsChecker == nil {
		return nil
	}

	users, err := am.Store.GetAccountUsers(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return fmt.Errorf("get account users for entitlement check: %w", err)
	}

	return entitlements.RequireLimit(ctx, am.entitlementsChecker, accountID, entitlements.LimitUsers, len(users)+1)
}

func (am *DefaultAccountManager) requireUserInviteLimitForCreate(ctx context.Context, accountID string, userCount int) error {
	if am.entitlementsChecker == nil {
		return nil
	}

	invites, err := am.Store.GetAccountUserInvites(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return fmt.Errorf("get account user invites for entitlement check: %w", err)
	}

	return entitlements.RequireLimit(ctx, am.entitlementsChecker, accountID, entitlements.LimitUsers, userCount+len(invites)+1)
}

func (am *DefaultAccountManager) requireUserLimitForCreateTx(ctx context.Context, transaction store.Store, accountID string) error {
	if am.entitlementsChecker == nil {
		return nil
	}

	users, err := transaction.GetAccountUsers(ctx, store.LockingStrengthUpdate, accountID)
	if err != nil {
		return fmt.Errorf("get account users for entitlement check: %w", err)
	}

	return entitlements.RequireLimit(ctx, am.entitlementsChecker, accountID, entitlements.LimitUsers, len(users)+1)
}

func (am *DefaultAccountManager) requirePeerLimitForCreateTx(ctx context.Context, transaction store.Store, accountID string) error {
	if am.entitlementsChecker == nil {
		return nil
	}

	peers, err := transaction.GetAccountPeers(ctx, store.LockingStrengthUpdate, accountID, "", "")
	if err != nil {
		return fmt.Errorf("get account peers for entitlement check: %w", err)
	}

	return entitlements.RequireLimit(ctx, am.entitlementsChecker, accountID, entitlements.LimitPeers, len(peers)+1)
}
