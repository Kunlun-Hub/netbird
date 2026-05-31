package entitlements

import "context"

type StaticProvider struct {
	defaultEntitlements Entitlements
	accountEntitlements map[string]Entitlements
}

func NewStaticProvider(defaultPlan Plan, accountEntitlements map[string]Entitlements) (*StaticProvider, error) {
	defaultEntitlements, err := PlanEntitlements(defaultPlan)
	if err != nil {
		return nil, err
	}

	accounts := make(map[string]Entitlements, len(accountEntitlements))
	for accountID, entitlements := range accountEntitlements {
		accounts[accountID] = entitlements.Clone()
	}

	return &StaticProvider{
		defaultEntitlements: defaultEntitlements,
		accountEntitlements: accounts,
	}, nil
}

func NewBasicStaticProvider() *StaticProvider {
	entitlements, _ := PlanEntitlements(PlanBasic)
	return &StaticProvider{defaultEntitlements: entitlements}
}

func NewProStaticProvider() *StaticProvider {
	entitlements, _ := PlanEntitlements(PlanPro)
	return &StaticProvider{defaultEntitlements: entitlements}
}

func (p *StaticProvider) GetEntitlements(_ context.Context, accountID string) (Entitlements, error) {
	if p.accountEntitlements != nil {
		if entitlements, ok := p.accountEntitlements[accountID]; ok {
			cloned := entitlements.Clone()
			cloned.AccountID = accountID
			return cloned, nil
		}
	}

	entitlements := p.defaultEntitlements.Clone()
	entitlements.AccountID = accountID
	return entitlements, nil
}
