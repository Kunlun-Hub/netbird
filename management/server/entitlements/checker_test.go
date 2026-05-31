package entitlements

import (
	"context"
	"testing"
)

func TestBasicPlanEntitlements(t *testing.T) {
	entitlements, err := PlanEntitlements(PlanBasic)
	if err != nil {
		t.Fatalf("PlanEntitlements() error = %v", err)
	}

	allowedFeatures := []Feature{
		FeatureLocalAuth,
		FeatureSelfHostedRelays,
		FeatureReverseProxy,
	}
	for _, feature := range allowedFeatures {
		if !entitlements.FeatureEnabled(feature) {
			t.Fatalf("basic feature %s should be allowed", feature)
		}
	}

	deniedFeatures := []Feature{
		FeatureIdentityProviders,
		FeatureJWTGroups,
		FeatureGroupsPropagation,
		FeatureFlowLogs,
		FeatureDNSLogs,
		FeatureDevicePosture,
		FeatureDNS,
		FeatureBranding,
		FeatureHARoutes,
		FeatureWebSSH,
		FeatureWebRDP,
	}
	for _, feature := range deniedFeatures {
		if entitlements.FeatureEnabled(feature) {
			t.Fatalf("basic feature %s should be denied", feature)
		}
	}

	expectedLimits := map[Limit]int{
		LimitSelfHostedRelays:   1,
		LimitUsers:              3,
		LimitPeers:              10,
		LimitReverseProxyServer: 1,
		LimitCustomDomains:      1,
		LimitCustomRules:        1,
	}
	for limit, want := range expectedLimits {
		if got := entitlements.LimitValue(limit); got != want {
			t.Fatalf("basic limit %s = %d, want %d", limit, got, want)
		}
	}
}

func TestProPlanEnablesAllKnownFeaturesAndUnlimitedLimits(t *testing.T) {
	entitlements, err := PlanEntitlements(PlanPro)
	if err != nil {
		t.Fatalf("PlanEntitlements() error = %v", err)
	}

	for feature, allowed := range entitlements.Features {
		if !allowed {
			t.Fatalf("pro feature %s should be allowed", feature)
		}
	}
	for limit, value := range entitlements.Limits {
		if value != Unlimited {
			t.Fatalf("pro limit %s = %d, want Unlimited", limit, value)
		}
	}
}

func TestCheckerUsesDefaultPlan(t *testing.T) {
	checker := NewChecker(NewBasicStaticProvider())

	decision, err := checker.IsAllowed(context.Background(), "account-a", FeatureIdentityProviders)
	if err != nil {
		t.Fatalf("IsAllowed() error = %v", err)
	}
	if decision.Allowed {
		t.Fatalf("FeatureIdentityProviders should be denied on basic plan")
	}

	limit, err := checker.Limit(context.Background(), "account-a", LimitPeers)
	if err != nil {
		t.Fatalf("Limit() error = %v", err)
	}
	if limit.Value != 10 {
		t.Fatalf("LimitPeers = %d, want 10", limit.Value)
	}
	if !limit.Allows(9, 1) {
		t.Fatalf("LimitPeers should allow one more peer at current=9")
	}
	if limit.Allows(10, 1) {
		t.Fatalf("LimitPeers should deny one more peer at current=10")
	}
}

func TestStaticProviderAccountOverride(t *testing.T) {
	pro, err := PlanEntitlements(PlanPro)
	if err != nil {
		t.Fatalf("PlanEntitlements() error = %v", err)
	}
	provider, err := NewStaticProvider(PlanBasic, map[string]Entitlements{
		"account-pro": pro,
	})
	if err != nil {
		t.Fatalf("NewStaticProvider() error = %v", err)
	}

	checker := NewChecker(provider)
	decision, err := checker.IsAllowed(context.Background(), "account-pro", FeatureBranding)
	if err != nil {
		t.Fatalf("IsAllowed() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("FeatureBranding should be allowed for account override")
	}
	if decision.Plan != PlanPro {
		t.Fatalf("Plan = %s, want %s", decision.Plan, PlanPro)
	}
}

func TestSnapshotReturnsAllKnownFeaturesAndLimits(t *testing.T) {
	snapshot, err := Snapshot(context.Background(), NewChecker(NewBasicStaticProvider()), "account-a")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.AccountID != "account-a" {
		t.Fatalf("AccountID = %s, want account-a", snapshot.AccountID)
	}
	if snapshot.Plan != PlanBasic {
		t.Fatalf("Plan = %s, want %s", snapshot.Plan, PlanBasic)
	}
	if len(snapshot.Features) != len(KnownFeatures()) {
		t.Fatalf("features len = %d, want %d", len(snapshot.Features), len(KnownFeatures()))
	}
	if len(snapshot.Limits) != len(KnownLimits()) {
		t.Fatalf("limits len = %d, want %d", len(snapshot.Limits), len(KnownLimits()))
	}
	if snapshot.Features[FeatureBranding] {
		t.Fatalf("FeatureBranding should be denied on basic plan")
	}
	if snapshot.Limits[LimitUsers] != 3 {
		t.Fatalf("LimitUsers = %d, want 3", snapshot.Limits[LimitUsers])
	}
}
