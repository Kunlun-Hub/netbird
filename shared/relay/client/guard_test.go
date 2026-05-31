package client

import (
	"testing"
	"time"

	retrypolicy "github.com/netbirdio/netbird/shared/retry"
)

func TestGuardReconnectPolicyUsesDefaultMaxInterval(t *testing.T) {
	guard := NewGuard(&ServerPicker{}, 0)

	policy := guard.reconnectPolicy()
	if policy.InitialInterval != retrypolicy.RelayReconnectPolicy.InitialInterval {
		t.Fatalf("InitialInterval = %s, want %s", policy.InitialInterval, retrypolicy.RelayReconnectPolicy.InitialInterval)
	}
	if policy.Multiplier != retrypolicy.RelayReconnectPolicy.Multiplier {
		t.Fatalf("Multiplier = %f, want %f", policy.Multiplier, retrypolicy.RelayReconnectPolicy.Multiplier)
	}
	if policy.RandomizationFactor != retrypolicy.RelayReconnectPolicy.RandomizationFactor {
		t.Fatalf("RandomizationFactor = %f, want %f", policy.RandomizationFactor, retrypolicy.RelayReconnectPolicy.RandomizationFactor)
	}
	if policy.MaxInterval != defaultMaxBackoffInterval {
		t.Fatalf("MaxInterval = %s, want %s", policy.MaxInterval, defaultMaxBackoffInterval)
	}
}

func TestGuardReconnectPolicyUsesConfiguredMaxInterval(t *testing.T) {
	configuredMax := 5 * time.Second
	guard := NewGuard(&ServerPicker{}, configuredMax)

	policy := guard.reconnectPolicy()
	if policy.MaxInterval != configuredMax {
		t.Fatalf("MaxInterval = %s, want %s", policy.MaxInterval, configuredMax)
	}
}
