package entitlements

import (
	"context"

	"github.com/netbirdio/netbird/shared/management/status"
)

func RequireFeature(ctx context.Context, checker Checker, accountID string, feature Feature) error {
	if checker == nil {
		return nil
	}

	decision, err := checker.IsAllowed(ctx, accountID, feature)
	if err != nil {
		return status.Errorf(status.Internal, "check feature entitlement: %v", err)
	}
	if !decision.Allowed {
		return status.ErrorfWithDetails(
			status.PermissionDenied,
			"feature_not_entitled",
			map[string]interface{}{
				"feature":       string(feature),
				"required_plan": string(PlanPro),
			},
			"feature_not_entitled: feature %s is not available on %s plan",
			feature,
			decision.Plan,
		)
	}
	return nil
}

func RequireLimit(ctx context.Context, checker Checker, accountID string, limit Limit, current int) error {
	if checker == nil {
		return nil
	}

	decision, err := checker.Limit(ctx, accountID, limit)
	if err != nil {
		return status.Errorf(status.Internal, "check limit entitlement: %v", err)
	}
	if decision.Value != Unlimited && current > decision.Value {
		return status.ErrorfWithDetails(
			status.PermissionDenied,
			"limit_exceeded",
			map[string]interface{}{
				"limit":         string(limit),
				"current":       current,
				"allowed":       decision.Value,
				"required_plan": string(PlanPro),
			},
			"limit_exceeded: limit %s allows %d, current %d on %s plan",
			limit,
			decision.Value,
			current,
			decision.Plan,
		)
	}
	return nil
}
