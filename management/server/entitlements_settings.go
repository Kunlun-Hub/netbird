package server

import (
	"context"
	"strings"

	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/types"
)

func (am *DefaultAccountManager) validateSettingsEntitlements(ctx context.Context, accountID string, settings *types.Settings) error {
	if settings == nil {
		return nil
	}

	if settings.JWTGroupsEnabled {
		if err := am.requireEntitledFeature(ctx, accountID, entitlements.FeatureJWTGroups); err != nil {
			return err
		}
	}
	if settings.GroupsPropagationEnabled {
		if err := am.requireEntitledFeature(ctx, accountID, entitlements.FeatureGroupsPropagation); err != nil {
			return err
		}
	}
	if settings.LoginMethod == types.LoginMethodWeChatWork || externalLoginOptionsEnabled(settings.EnabledLoginOptions) {
		if err := am.requireEntitledFeature(ctx, accountID, entitlements.FeatureIdentityProviders); err != nil {
			return err
		}
	}
	if settings.RoutingPeerDNSResolutionEnabled || settings.DNSDomain != "" {
		if err := am.requireEntitledFeature(ctx, accountID, entitlements.FeatureDNS); err != nil {
			return err
		}
	}

	return am.validateExtraSettingsEntitlements(ctx, accountID, settings.Extra)
}

func (am *DefaultAccountManager) validateExtraSettingsEntitlements(ctx context.Context, accountID string, extra *types.ExtraSettings) error {
	if extra == nil {
		return nil
	}

	if flowLogsEnabled(extra) {
		if err := am.requireEntitledFeature(ctx, accountID, entitlements.FeatureFlowLogs); err != nil {
			return err
		}
	}
	if extra.FlowDnsCollectionEnabled {
		if err := am.requireEntitledFeature(ctx, accountID, entitlements.FeatureDNSLogs); err != nil {
			return err
		}
	}
	if brandingEnabled(extra) {
		if err := am.requireEntitledFeature(ctx, accountID, entitlements.FeatureBranding); err != nil {
			return err
		}
	}
	relayReferenceCount := countRelayReferences(extra)
	if relayReferenceCount > 0 {
		if err := am.requireEntitledFeature(ctx, accountID, entitlements.FeatureSelfHostedRelays); err != nil {
			return err
		}
		if err := am.requireEntitledLimit(ctx, accountID, entitlements.LimitSelfHostedRelays, relayReferenceCount); err != nil {
			return err
		}
	}

	return nil
}

func (am *DefaultAccountManager) requireEntitledFeature(ctx context.Context, accountID string, feature entitlements.Feature) error {
	return entitlements.RequireFeature(ctx, am.entitlementsChecker, accountID, feature)
}

func (am *DefaultAccountManager) requireEntitledLimit(ctx context.Context, accountID string, limit entitlements.Limit, current int) error {
	return entitlements.RequireLimit(ctx, am.entitlementsChecker, accountID, limit, current)
}

func flowLogsEnabled(extra *types.ExtraSettings) bool {
	return extra.FlowEnabled ||
		len(extra.FlowGroups) > 0 ||
		extra.FlowPacketCounterEnabled ||
		extra.FlowENCollectionEnabled ||
		extra.FlowLocalStorageEnabled ||
		extra.FlowLocalStoragePath != "" ||
		extra.FlowLocalStorageMaxSizeMB > 0 ||
		extra.FlowLocalStorageMaxFiles > 0 ||
		extra.FlowSyslogEnabled ||
		extra.FlowSyslogServer != "" ||
		extra.FlowSyslogProtocol != "" ||
		extra.FlowSyslogFacility != "" ||
		extra.FlowSyslogTag != ""
}

func brandingEnabled(extra *types.ExtraSettings) bool {
	return extra.BrandingLogoDataURL != "" ||
		extra.BrandingLogoDarkDataURL != "" ||
		extra.BrandingIconDataURL != "" ||
		extra.BrandingTabTitle != "" ||
		extra.BrandingPrimaryColor != ""
}

func countRelayReferences(extra *types.ExtraSettings) int {
	canonicalByAlias := make(map[string]string, len(extra.RegisteredRelays)*3)
	refs := make(map[string]struct{}, len(extra.RegisteredRelays))
	for key, relay := range extra.RegisteredRelays {
		canonical := firstRelayAlias(relay.ID, key, relay.Address)
		if canonical == "" {
			continue
		}
		refs[canonical] = struct{}{}
		for _, alias := range []string{key, relay.ID, relay.Address} {
			alias = strings.TrimSpace(alias)
			if alias != "" {
				canonicalByAlias[alias] = canonical
			}
		}
	}
	for _, relays := range extra.RelayPeerPreferences {
		for _, relay := range relays {
			addRelayReference(refs, canonicalByAlias, relay)
		}
	}
	for _, relays := range extra.RelayGroupPreferences {
		for _, relay := range relays {
			addRelayReference(refs, canonicalByAlias, relay)
		}
	}
	return len(refs)
}

func firstRelayAlias(aliases ...string) string {
	for _, alias := range aliases {
		if alias = strings.TrimSpace(alias); alias != "" {
			return alias
		}
	}
	return ""
}

func addRelayReference(refs map[string]struct{}, canonicalByAlias map[string]string, relay string) {
	relay = strings.TrimSpace(relay)
	if relay == "" {
		return
	}
	if canonical, ok := canonicalByAlias[relay]; ok {
		refs[canonical] = struct{}{}
		return
	}
	refs[relay] = struct{}{}
}

func externalLoginOptionsEnabled(options []types.LoginOption) bool {
	for _, option := range options {
		if option.IsProviderLoginOption() {
			return true
		}
	}
	return false
}
