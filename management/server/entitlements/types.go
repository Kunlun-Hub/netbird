package entitlements

import "fmt"

// Plan identifies the account subscription tier used for feature entitlements.
type Plan string

const (
	PlanBasic Plan = "basic"
	PlanPro   Plan = "pro"
)

// Feature identifies a binary account-level capability.
type Feature string

const (
	FeatureLocalAuth         Feature = "local_auth"
	FeatureIdentityProviders Feature = "identity_providers"
	FeatureJWTGroups         Feature = "jwt_groups"
	FeatureGroupsPropagation Feature = "groups_propagation"
	FeatureFlowLogs          Feature = "flow_logs"
	FeatureDNSLogs           Feature = "dns_logs"
	FeatureSelfHostedRelays  Feature = "self_hosted_relays"
	FeatureReverseProxy      Feature = "reverse_proxy"
	FeatureDevicePosture     Feature = "device_posture"
	FeatureDNS               Feature = "dns"
	FeatureBranding          Feature = "branding"
	FeatureHARoutes          Feature = "ha_routes"
	FeatureWebSSH            Feature = "web_ssh"
	FeatureWebRDP            Feature = "web_rdp"
)

var knownFeatures = []Feature{
	FeatureLocalAuth,
	FeatureIdentityProviders,
	FeatureJWTGroups,
	FeatureGroupsPropagation,
	FeatureFlowLogs,
	FeatureDNSLogs,
	FeatureSelfHostedRelays,
	FeatureReverseProxy,
	FeatureDevicePosture,
	FeatureDNS,
	FeatureBranding,
	FeatureHARoutes,
	FeatureWebSSH,
	FeatureWebRDP,
}

func KnownFeatures() []Feature {
	return append([]Feature(nil), knownFeatures...)
}

// Limit identifies a numeric account-level quota.
type Limit string

const (
	LimitSelfHostedRelays   Limit = "self_hosted_relays"
	LimitUsers              Limit = "users"
	LimitPeers              Limit = "peers"
	LimitReverseProxyServer Limit = "reverse_proxy_servers"
	LimitCustomDomains      Limit = "custom_domains"
	LimitCustomRules        Limit = "custom_rules"
)

var knownLimits = []Limit{
	LimitSelfHostedRelays,
	LimitUsers,
	LimitPeers,
	LimitReverseProxyServer,
	LimitCustomDomains,
	LimitCustomRules,
}

func KnownLimits() []Limit {
	return append([]Limit(nil), knownLimits...)
}

const Unlimited = -1

// Decision is returned by feature checks.
type Decision struct {
	AccountID string
	Plan      Plan
	Feature   Feature
	Allowed   bool
}

// LimitDecision is returned by quota checks.
type LimitDecision struct {
	AccountID string
	Plan      Plan
	Limit     Limit
	Value     int
}

func (d LimitDecision) Allows(current, requested int) bool {
	if d.Value == Unlimited {
		return true
	}
	return current+requested <= d.Value
}

// Entitlements is a normalized feature and limit snapshot for one account.
type Entitlements struct {
	AccountID string
	Plan      Plan
	Features  map[Feature]bool
	Limits    map[Limit]int
	Usage     map[Limit]int
}

func (e Entitlements) FeatureEnabled(feature Feature) bool {
	return e.Features[feature]
}

func (e Entitlements) LimitValue(limit Limit) int {
	value, ok := e.Limits[limit]
	if !ok {
		return 0
	}
	return value
}

func (e Entitlements) Clone() Entitlements {
	return Entitlements{
		AccountID: e.AccountID,
		Plan:      e.Plan,
		Features:  cloneFeatureMap(e.Features),
		Limits:    cloneLimitMap(e.Limits),
		Usage:     cloneLimitMap(e.Usage),
	}
}

func PlanEntitlements(plan Plan) (Entitlements, error) {
	switch plan {
	case PlanBasic:
		return basicEntitlements(), nil
	case PlanPro:
		return proEntitlements(), nil
	default:
		return Entitlements{}, fmt.Errorf("unknown entitlement plan %q", plan)
	}
}

func basicEntitlements() Entitlements {
	return Entitlements{
		Plan: PlanBasic,
		Features: map[Feature]bool{
			FeatureLocalAuth:         true,
			FeatureIdentityProviders: false,
			FeatureJWTGroups:         false,
			FeatureGroupsPropagation: false,
			FeatureFlowLogs:          false,
			FeatureDNSLogs:           false,
			FeatureSelfHostedRelays:  true,
			FeatureReverseProxy:      true,
			FeatureDevicePosture:     false,
			FeatureDNS:               false,
			FeatureBranding:          false,
			FeatureHARoutes:          false,
			FeatureWebSSH:            false,
			FeatureWebRDP:            false,
		},
		Limits: map[Limit]int{
			LimitSelfHostedRelays:   1,
			LimitUsers:              3,
			LimitPeers:              10,
			LimitReverseProxyServer: 1,
			LimitCustomDomains:      1,
			LimitCustomRules:        3,
		},
	}
}

func proEntitlements() Entitlements {
	entitlements := basicEntitlements()
	entitlements.Plan = PlanPro
	for feature := range entitlements.Features {
		entitlements.Features[feature] = true
	}
	for limit := range entitlements.Limits {
		entitlements.Limits[limit] = Unlimited
	}
	return entitlements
}

func cloneFeatureMap(source map[Feature]bool) map[Feature]bool {
	if source == nil {
		return nil
	}
	result := make(map[Feature]bool, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneLimitMap(source map[Limit]int) map[Limit]int {
	if source == nil {
		return nil
	}
	result := make(map[Limit]int, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
