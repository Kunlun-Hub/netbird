package entitlements

import "context"

type Provider interface {
	GetEntitlements(ctx context.Context, accountID string) (Entitlements, error)
}

type Checker interface {
	IsAllowed(ctx context.Context, accountID string, feature Feature) (Decision, error)
	Limit(ctx context.Context, accountID string, limit Limit) (LimitDecision, error)
}

type DefaultChecker struct {
	provider Provider
}

func NewChecker(provider Provider) *DefaultChecker {
	return &DefaultChecker{provider: provider}
}

func (c *DefaultChecker) IsAllowed(ctx context.Context, accountID string, feature Feature) (Decision, error) {
	entitlements, err := c.provider.GetEntitlements(ctx, accountID)
	if err != nil {
		return Decision{}, err
	}
	return Decision{
		AccountID: accountID,
		Plan:      entitlements.Plan,
		Feature:   feature,
		Allowed:   entitlements.FeatureEnabled(feature),
	}, nil
}

func (c *DefaultChecker) Limit(ctx context.Context, accountID string, limit Limit) (LimitDecision, error) {
	entitlements, err := c.provider.GetEntitlements(ctx, accountID)
	if err != nil {
		return LimitDecision{}, err
	}
	return LimitDecision{
		AccountID: accountID,
		Plan:      entitlements.Plan,
		Limit:     limit,
		Value:     entitlements.LimitValue(limit),
	}, nil
}
