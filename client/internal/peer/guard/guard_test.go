package guard

import (
	"testing"
	"time"

	retrypolicy "github.com/netbirdio/netbird/shared/retry"
)

func TestGuardPoliciesUseSharedProfilesWithPeerTimeout(t *testing.T) {
	timeout := 42 * time.Second
	g := NewGuard(nil, nil, timeout, nil)

	initial := g.initialPolicy()
	if initial.InitialInterval != retrypolicy.PeerInitialPolicy.InitialInterval {
		t.Fatalf("initial InitialInterval = %s, want %s", initial.InitialInterval, retrypolicy.PeerInitialPolicy.InitialInterval)
	}
	if initial.RandomizationFactor != retrypolicy.PeerInitialPolicy.RandomizationFactor {
		t.Fatalf("initial RandomizationFactor = %f, want %f", initial.RandomizationFactor, retrypolicy.PeerInitialPolicy.RandomizationFactor)
	}
	if initial.Multiplier != retrypolicy.PeerInitialPolicy.Multiplier {
		t.Fatalf("initial Multiplier = %f, want %f", initial.Multiplier, retrypolicy.PeerInitialPolicy.Multiplier)
	}
	if initial.MaxInterval != timeout {
		t.Fatalf("initial MaxInterval = %s, want %s", initial.MaxInterval, timeout)
	}

	reconnect := g.reconnectPolicy()
	if reconnect.InitialInterval != retrypolicy.PeerReconnectPolicy.InitialInterval {
		t.Fatalf("reconnect InitialInterval = %s, want %s", reconnect.InitialInterval, retrypolicy.PeerReconnectPolicy.InitialInterval)
	}
	if reconnect.RandomizationFactor != retrypolicy.PeerReconnectPolicy.RandomizationFactor {
		t.Fatalf("reconnect RandomizationFactor = %f, want %f", reconnect.RandomizationFactor, retrypolicy.PeerReconnectPolicy.RandomizationFactor)
	}
	if reconnect.Multiplier != retrypolicy.PeerReconnectPolicy.Multiplier {
		t.Fatalf("reconnect Multiplier = %f, want %f", reconnect.Multiplier, retrypolicy.PeerReconnectPolicy.Multiplier)
	}
	if reconnect.MaxInterval != timeout {
		t.Fatalf("reconnect MaxInterval = %s, want %s", reconnect.MaxInterval, timeout)
	}
}
