package retry

import (
	"testing"
	"time"
)

func TestPolicyWithMaxInterval(t *testing.T) {
	original := RelayReconnectPolicy
	updated := original.WithMaxInterval(5 * time.Second)

	if updated.MaxInterval != 5*time.Second {
		t.Fatalf("MaxInterval = %s, want 5s", updated.MaxInterval)
	}
	if original.MaxInterval == updated.MaxInterval {
		t.Fatalf("WithMaxInterval mutated original policy")
	}
}

func TestRelayReconnectPolicyMatchesLegacyProfile(t *testing.T) {
	if RelayReconnectPolicy.InitialInterval != 2*time.Second {
		t.Fatalf("InitialInterval = %s, want 2s", RelayReconnectPolicy.InitialInterval)
	}
	if RelayReconnectPolicy.RandomizationFactor != 0 {
		t.Fatalf("RandomizationFactor = %f, want 0", RelayReconnectPolicy.RandomizationFactor)
	}
	if RelayReconnectPolicy.Multiplier != 2 {
		t.Fatalf("Multiplier = %f, want 2", RelayReconnectPolicy.Multiplier)
	}
	if RelayReconnectPolicy.MaxInterval != 60*time.Second {
		t.Fatalf("MaxInterval = %s, want 1m", RelayReconnectPolicy.MaxInterval)
	}
	if RelayReconnectPolicy.MaxElapsedTime != 0 {
		t.Fatalf("MaxElapsedTime = %s, want 0", RelayReconnectPolicy.MaxElapsedTime)
	}
}
