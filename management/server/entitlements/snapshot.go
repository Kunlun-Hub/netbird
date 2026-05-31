package entitlements

import "context"

func Snapshot(ctx context.Context, checker Checker, accountID string) (Entitlements, error) {
	if checker == nil {
		entitlements, err := PlanEntitlements(PlanPro)
		if err != nil {
			return Entitlements{}, err
		}
		entitlements.AccountID = accountID
		return entitlements, nil
	}

	features := make(map[Feature]bool, len(knownFeatures))
	limits := make(map[Limit]int, len(knownLimits))
	plan := PlanBasic

	for _, feature := range knownFeatures {
		decision, err := checker.IsAllowed(ctx, accountID, feature)
		if err != nil {
			return Entitlements{}, err
		}
		plan = decision.Plan
		features[feature] = decision.Allowed
	}

	for _, limit := range knownLimits {
		decision, err := checker.Limit(ctx, accountID, limit)
		if err != nil {
			return Entitlements{}, err
		}
		plan = decision.Plan
		limits[limit] = decision.Value
	}

	return Entitlements{
		AccountID: accountID,
		Plan:      plan,
		Features:  features,
		Limits:    limits,
	}, nil
}
