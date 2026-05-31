package server

import (
	"context"
	"strings"
	"testing"

	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

func TestValidateSettingsEntitlementsBasicDeniesFlowLogs(t *testing.T) {
	manager := basicEntitlementsAccountManager()

	err := manager.validateSettingsEntitlements(context.Background(), "account-a", &types.Settings{
		Extra: &types.ExtraSettings{FlowEnabled: true},
	})

	assertFeatureDenied(t, err, entitlements.FeatureFlowLogs)
}

func TestValidateSettingsEntitlementsBasicDeniesDNSLogs(t *testing.T) {
	manager := basicEntitlementsAccountManager()

	err := manager.validateSettingsEntitlements(context.Background(), "account-a", &types.Settings{
		Extra: &types.ExtraSettings{FlowDnsCollectionEnabled: true},
	})

	assertFeatureDenied(t, err, entitlements.FeatureDNSLogs)
}

func TestValidateSettingsEntitlementsBasicDeniesDNSSettings(t *testing.T) {
	manager := basicEntitlementsAccountManager()

	err := manager.validateSettingsEntitlements(context.Background(), "account-a", &types.Settings{
		DNSDomain: "example.test",
	})

	assertFeatureDenied(t, err, entitlements.FeatureDNS)
}

func TestValidateSettingsEntitlementsBasicDeniesExternalLoginOptions(t *testing.T) {
	manager := basicEntitlementsAccountManager()

	err := manager.validateSettingsEntitlements(context.Background(), "account-a", &types.Settings{
		EnabledLoginOptions: []types.LoginOption{
			types.LoginOptionEmail,
			types.CreateProviderLoginOption("oidc"),
		},
	})

	assertFeatureDenied(t, err, entitlements.FeatureIdentityProviders)
}

func TestValidateSettingsEntitlementsBasicDeniesBranding(t *testing.T) {
	manager := basicEntitlementsAccountManager()

	err := manager.validateSettingsEntitlements(context.Background(), "account-a", &types.Settings{
		Extra: &types.ExtraSettings{BrandingTabTitle: "Cloink"},
	})

	assertFeatureDenied(t, err, entitlements.FeatureBranding)
}

func TestValidateSettingsEntitlementsBasicLimitsRegisteredRelays(t *testing.T) {
	manager := basicEntitlementsAccountManager()

	err := manager.validateSettingsEntitlements(context.Background(), "account-a", &types.Settings{
		Extra: &types.ExtraSettings{RegisteredRelays: map[string]types.RegisteredRelay{
			"relay-a": {ID: "relay-a"},
			"relay-b": {ID: "relay-b"},
		}},
	})

	if err == nil {
		t.Fatalf("expected registered relay limit error")
	}
	if !strings.Contains(err.Error(), "limit_exceeded") {
		t.Fatalf("error = %q, want limit_exceeded", err)
	}
}

func TestValidateSettingsEntitlementsBasicAllowsOneRegisteredRelay(t *testing.T) {
	manager := basicEntitlementsAccountManager()

	err := manager.validateSettingsEntitlements(context.Background(), "account-a", &types.Settings{
		Extra: &types.ExtraSettings{RegisteredRelays: map[string]types.RegisteredRelay{
			"relay-a": {ID: "relay-a"},
		}},
	})
	if err != nil {
		t.Fatalf("validateSettingsEntitlements() error = %v", err)
	}
}

func TestValidateSettingsEntitlementsBasicLimitsRelayPreferences(t *testing.T) {
	manager := basicEntitlementsAccountManager()

	err := manager.validateSettingsEntitlements(context.Background(), "account-a", &types.Settings{
		Extra: &types.ExtraSettings{
			RegisteredRelays: map[string]types.RegisteredRelay{
				"relay-a": {ID: "relay-a"},
			},
			RelayPeerPreferences: map[string][]string{
				"peer-a": {"relay-b"},
			},
		},
	})

	if err == nil {
		t.Fatalf("expected relay preference limit error")
	}
	if !strings.Contains(err.Error(), "limit_exceeded") {
		t.Fatalf("error = %q, want limit_exceeded", err)
	}
}

func TestValidateSettingsEntitlementsBasicTreatsRegisteredRelayAddressPreferenceAsSameRelay(t *testing.T) {
	manager := basicEntitlementsAccountManager()

	err := manager.validateSettingsEntitlements(context.Background(), "account-a", &types.Settings{
		Extra: &types.ExtraSettings{
			RegisteredRelays: map[string]types.RegisteredRelay{
				"relay-key": {Address: "rels://relay.example.com"},
			},
			RelayPeerPreferences: map[string][]string{
				"peer-a": {"rels://relay.example.com"},
			},
		},
	})
	if err != nil {
		t.Fatalf("validateSettingsEntitlements() error = %v", err)
	}
}

func TestValidateSettingsEntitlementsProAllowsAdvancedSettings(t *testing.T) {
	pro, err := entitlements.PlanEntitlements(entitlements.PlanPro)
	if err != nil {
		t.Fatalf("PlanEntitlements() error = %v", err)
	}
	provider, err := entitlements.NewStaticProvider(entitlements.PlanBasic, map[string]entitlements.Entitlements{
		"account-pro": pro,
	})
	if err != nil {
		t.Fatalf("NewStaticProvider() error = %v", err)
	}

	manager := &DefaultAccountManager{
		entitlementsChecker: entitlements.NewChecker(provider),
	}
	err = manager.validateSettingsEntitlements(context.Background(), "account-pro", &types.Settings{
		DNSDomain:                "example.test",
		JWTGroupsEnabled:         true,
		GroupsPropagationEnabled: true,
		Extra: &types.ExtraSettings{
			FlowEnabled:              true,
			FlowDnsCollectionEnabled: true,
			BrandingTabTitle:         "Cloink",
			RegisteredRelays: map[string]types.RegisteredRelay{
				"relay-a": {ID: "relay-a"},
				"relay-b": {ID: "relay-b"},
			},
		},
	})
	if err != nil {
		t.Fatalf("validateSettingsEntitlements() error = %v", err)
	}
}

func basicEntitlementsAccountManager() *DefaultAccountManager {
	return &DefaultAccountManager{
		entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider()),
	}
}

func assertFeatureDenied(t *testing.T, err error, feature entitlements.Feature) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected feature %s to be denied", feature)
	}
	sErr, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error = %T %v, want status error", err, err)
	}
	if sErr.Type() != status.PermissionDenied {
		t.Fatalf("error type = %v, want PermissionDenied", sErr.Type())
	}
	if !strings.Contains(err.Error(), string(feature)) {
		t.Fatalf("error = %q, want feature %s", err, feature)
	}
}
