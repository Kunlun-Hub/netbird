package controller

import (
	"context"
	"testing"

	nbdns "github.com/netbirdio/netbird/dns"
	"github.com/netbirdio/netbird/management/server/entitlements"
	nbpeer "github.com/netbirdio/netbird/management/server/peer"
	"github.com/netbirdio/netbird/management/server/posture"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/route"
)

func TestFilterAccountForEntitlementsBasicStripsDNSPostureAndWebSSH(t *testing.T) {
	account := &types.Account{
		Id:      "account-a",
		Network: types.NewNetwork(),
		Peers: map[string]*nbpeer.Peer{
			"peer-a": {ID: "peer-a", SSHEnabled: true},
		},
		Users:  map[string]*types.User{},
		Groups: map[string]*types.Group{},
		Routes: map[route.ID]*route.Route{},
		NameServerGroups: map[string]*nbdns.NameServerGroup{
			"ns-a": {ID: "ns-a", Enabled: true},
		},
		Settings: &types.Settings{
			DNSDomain:                       "example.test",
			RoutingPeerDNSResolutionEnabled: true,
		},
		PostureChecks: []*posture.Checks{{ID: "posture-a"}},
		Policies: []*types.Policy{
			{
				ID:                  "policy-a",
				SourcePostureChecks: []string{"posture-a"},
				Rules: []*types.PolicyRule{
					{ID: "ssh-rule", Protocol: types.PolicyRuleProtocolNetbirdSSH},
					{ID: "tcp-rule", Protocol: types.PolicyRuleProtocolTCP},
				},
			},
		},
	}

	controller := &Controller{
		entitlementsChecker: entitlements.NewChecker(entitlements.NewBasicStaticProvider()),
	}

	filtered, dnsEnabled, err := controller.filterAccountForEntitlements(context.Background(), account)
	if err != nil {
		t.Fatalf("filterAccountForEntitlements() error = %v", err)
	}
	if dnsEnabled {
		t.Fatalf("dnsEnabled = true, want false")
	}
	if len(filtered.NameServerGroups) != 0 {
		t.Fatalf("NameServerGroups len = %d, want 0", len(filtered.NameServerGroups))
	}
	if filtered.Settings.DNSDomain != "" || filtered.Settings.RoutingPeerDNSResolutionEnabled {
		t.Fatalf("DNS settings were not stripped: %+v", filtered.Settings)
	}
	if len(filtered.PostureChecks) != 0 || len(filtered.Policies[0].SourcePostureChecks) != 0 {
		t.Fatalf("posture settings were not stripped")
	}
	if filtered.Peers["peer-a"].SSHEnabled {
		t.Fatalf("peer SSHEnabled = true, want false")
	}
	if len(filtered.Policies[0].Rules) != 1 || filtered.Policies[0].Rules[0].ID != "tcp-rule" {
		t.Fatalf("netbird-ssh rule was not stripped: %+v", filtered.Policies[0].Rules)
	}
	if !account.Peers["peer-a"].SSHEnabled {
		t.Fatalf("original account was mutated")
	}
}
