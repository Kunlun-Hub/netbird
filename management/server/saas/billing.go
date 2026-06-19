package saas

import (
	"context"

	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

type BillingService struct {
	Store store.Store
}

type BillResponse struct {
	Bill  *types.SaaSBill       `json:"bill"`
	Items []*types.SaaSBillItem `json:"items"`
}

func (s BillingService) ListBills(ctx context.Context, accountID string) ([]BillResponse, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if accountID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id is required")
	}
	if _, err := s.Store.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthNone, accountID); err != nil {
		return nil, err
	}
	bills, err := s.Store.ListSaaSBills(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}
	resp := make([]BillResponse, 0, len(bills))
	for _, bill := range bills {
		items, err := s.Store.ListSaaSBillItems(ctx, store.LockingStrengthNone, bill.ID)
		if err != nil {
			return nil, err
		}
		resp = append(resp, BillResponse{Bill: bill, Items: items})
	}
	return resp, nil
}
