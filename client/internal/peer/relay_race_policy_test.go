package peer

import (
	"testing"
	"time"
)

func TestP2PFailureCache(t *testing.T) {
	cache := newP2PFailureCache(time.Minute, 2)
	now := time.Now()
	peerKey := "peer-a"

	if cache.shouldPreferRelay(peerKey, now) {
		t.Fatal("new peer should not prefer relay")
	}

	cache.markFailure(peerKey, now)
	if cache.shouldPreferRelay(peerKey, now) {
		t.Fatal("single failure should not prefer relay")
	}

	cache.markFailure(peerKey, now.Add(time.Second))
	if !cache.shouldPreferRelay(peerKey, now.Add(2*time.Second)) {
		t.Fatal("threshold failures should prefer relay")
	}

	cache.markSuccess(peerKey)
	if cache.shouldPreferRelay(peerKey, now.Add(3*time.Second)) {
		t.Fatal("success should clear relay preference")
	}
}

func TestP2PFailureCacheTTL(t *testing.T) {
	cache := newP2PFailureCache(time.Minute, 1)
	now := time.Now()
	peerKey := "peer-a"

	cache.markFailure(peerKey, now)
	if !cache.shouldPreferRelay(peerKey, now.Add(30*time.Second)) {
		t.Fatal("fresh failure should prefer relay")
	}

	if cache.shouldPreferRelay(peerKey, now.Add(2*time.Minute)) {
		t.Fatal("expired failure should not prefer relay")
	}
}

func TestRelayRacePolicyDelay(t *testing.T) {
	policy := defaultRelayRacePolicy()

	if got := policy.relayDelay(false); got != defaultRelayWarmupDelay {
		t.Fatalf("relayDelay(false) = %s, want %s", got, defaultRelayWarmupDelay)
	}
	if got := policy.relayDelay(true); got != 0 {
		t.Fatalf("relayDelay(true) = %s, want 0", got)
	}
	if got, want := policy.relayActivationBudget(false), defaultRelayActivationDelay-defaultRelayWarmupDelay; got != want {
		t.Fatalf("relayActivationBudget(false) = %s, want %s", got, want)
	}
	if got := policy.relayActivationBudget(true); got != 0 {
		t.Fatalf("relayActivationBudget(true) = %s, want 0", got)
	}
}
