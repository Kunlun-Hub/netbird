package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"

	rpservice "github.com/netbirdio/netbird/management/internals/modules/reverseproxy/service"
	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/store"
)

func TestValidateServiceEntitlementsBasicDeniesFourthService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountServices(gomock.Any(), store.LockingStrengthUpdate, "account-a").
		Return([]*rpservice.Service{
			{ID: "existing-a", Targets: []*rpservice.Target{{}}},
			{ID: "existing-b", Targets: []*rpservice.Target{{}}},
			{ID: "existing-c", Targets: []*rpservice.Target{{}}},
		}, nil)

	mgr := &Manager{entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider())}
	err := mgr.validateServiceEntitlements(context.Background(), mockStore, "account-a", &rpservice.Service{
		ID:      "new",
		Targets: []*rpservice.Target{{}},
	})

	assertReverseProxyLimitDenied(t, err, entitlements.LimitCustomRules)
}

func TestValidateServiceEntitlementsBasicAllowsThirdServiceWithMultipleTargets(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountServices(gomock.Any(), store.LockingStrengthUpdate, "account-a").
		Return([]*rpservice.Service{
			{ID: "existing-a", Targets: []*rpservice.Target{{}}},
			{ID: "existing-b", Targets: []*rpservice.Target{{}}},
		}, nil)

	mgr := &Manager{entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider())}
	err := mgr.validateServiceEntitlements(context.Background(), mockStore, "account-a", &rpservice.Service{
		ID:      "new",
		Targets: []*rpservice.Target{{}, {}, {}, {}},
	})
	if err != nil {
		t.Fatalf("validateServiceEntitlements() error = %v", err)
	}
}

func assertReverseProxyLimitDenied(t *testing.T, err error, limit entitlements.Limit) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected limit %s to be denied", limit)
	}
	if !strings.Contains(err.Error(), "limit_exceeded") || !strings.Contains(err.Error(), string(limit)) {
		t.Fatalf("error = %q, want limit_exceeded for %s", err, limit)
	}
}
