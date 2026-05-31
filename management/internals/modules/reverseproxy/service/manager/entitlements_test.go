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

func TestValidateServiceEntitlementsBasicDeniesSecondService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountServices(gomock.Any(), store.LockingStrengthUpdate, "account-a").
		Return([]*rpservice.Service{{ID: "existing", Targets: []*rpservice.Target{{}}}}, nil)

	mgr := &Manager{entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider())}
	err := mgr.validateServiceEntitlements(context.Background(), mockStore, "account-a", &rpservice.Service{
		ID:      "new",
		Targets: []*rpservice.Target{{}},
	})

	assertReverseProxyLimitDenied(t, err, entitlements.LimitReverseProxyServer)
}

func TestValidateServiceEntitlementsBasicDeniesMoreThanThreeCustomRules(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountServices(gomock.Any(), store.LockingStrengthUpdate, "account-a").
		Return(nil, nil)

	mgr := &Manager{entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider())}
	err := mgr.validateServiceEntitlements(context.Background(), mockStore, "account-a", &rpservice.Service{
		ID:      "new",
		Targets: []*rpservice.Target{{}, {}, {}, {}},
	})

	assertReverseProxyLimitDenied(t, err, entitlements.LimitCustomRules)
}

func TestValidateServiceEntitlementsBasicAllowsOneServiceWithThreeCustomRules(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := store.NewMockStore(ctrl)
	mockStore.EXPECT().
		GetAccountServices(gomock.Any(), store.LockingStrengthUpdate, "account-a").
		Return(nil, nil)

	mgr := &Manager{entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider())}
	err := mgr.validateServiceEntitlements(context.Background(), mockStore, "account-a", &rpservice.Service{
		ID:      "new",
		Targets: []*rpservice.Target{{}, {}, {}},
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
