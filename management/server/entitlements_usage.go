package server

import (
	"context"
	"fmt"

	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/store"
)

func (am *DefaultAccountManager) accountEntitlementUsage(ctx context.Context, accountID string) (map[entitlements.Limit]int, error) {
	usage := map[entitlements.Limit]int{
		entitlements.LimitUsers:              0,
		entitlements.LimitPeers:              0,
		entitlements.LimitSelfHostedRelays:   0,
		entitlements.LimitReverseProxyServer: 0,
		entitlements.LimitCustomDomains:      0,
		entitlements.LimitCustomRules:        0,
	}
	if am.Store == nil {
		return usage, nil
	}

	users, err := am.Store.GetAccountUsers(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account users for entitlement usage: %w", err)
	}
	usage[entitlements.LimitUsers] = len(users)

	peers, err := am.Store.GetAccountPeers(ctx, store.LockingStrengthNone, accountID, "", "")
	if err != nil {
		return nil, fmt.Errorf("get account peers for entitlement usage: %w", err)
	}
	usage[entitlements.LimitPeers] = len(peers)

	settings, err := am.Store.GetAccountSettings(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account settings for entitlement usage: %w", err)
	}
	if settings != nil && settings.Extra != nil {
		usage[entitlements.LimitSelfHostedRelays] = countRelayReferences(settings.Extra)
	}

	services, err := am.Store.GetAccountServices(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account services for entitlement usage: %w", err)
	}
	for _, service := range services {
		if service == nil || service.Terminated {
			continue
		}
		usage[entitlements.LimitReverseProxyServer]++
		usage[entitlements.LimitCustomRules] += len(service.Targets)
	}

	domains, err := am.Store.ListCustomDomains(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get custom domains for entitlement usage: %w", err)
	}
	usage[entitlements.LimitCustomDomains] = len(domains)

	return usage, nil
}
