package peer

import (
	"sync"
	"time"
)

const (
	defaultRelayWarmupDelay     = 150 * time.Millisecond
	defaultRelayActivationDelay = 600 * time.Millisecond
	defaultP2PFailureTTL        = 10 * time.Minute
	defaultP2PFailureThreshold  = 2
)

type relayRacePolicy struct {
	relayWarmupDelay     time.Duration
	relayActivationDelay time.Duration
	p2pFailureTTL        time.Duration
	p2pFailureThreshold  int
}

func defaultRelayRacePolicy() relayRacePolicy {
	return relayRacePolicy{
		relayWarmupDelay:     defaultRelayWarmupDelay,
		relayActivationDelay: defaultRelayActivationDelay,
		p2pFailureTTL:        defaultP2PFailureTTL,
		p2pFailureThreshold:  defaultP2PFailureThreshold,
	}
}

func (p relayRacePolicy) relayDelay(fastRelay bool) time.Duration {
	if fastRelay {
		return 0
	}
	return p.relayWarmupDelay
}

func (p relayRacePolicy) relayActivationBudget(fastRelay bool) time.Duration {
	if fastRelay {
		return 0
	}
	if p.relayActivationDelay <= p.relayWarmupDelay {
		return 0
	}
	return p.relayActivationDelay - p.relayWarmupDelay
}

type p2pFailureCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	threshold int
	entries   map[string]p2pFailureEntry
}

type p2pFailureEntry struct {
	failures int
	last     time.Time
}

func newP2PFailureCache(ttl time.Duration, threshold int) *p2pFailureCache {
	return &p2pFailureCache{
		ttl:       ttl,
		threshold: threshold,
		entries:   make(map[string]p2pFailureEntry),
	}
}

func (c *p2pFailureCache) shouldPreferRelay(peerKey string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[peerKey]
	if !ok {
		return false
	}
	if now.Sub(entry.last) > c.ttl {
		delete(c.entries, peerKey)
		return false
	}
	return entry.failures >= c.threshold
}

func (c *p2pFailureCache) markFailure(peerKey string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := c.entries[peerKey]
	if now.Sub(entry.last) > c.ttl {
		entry.failures = 0
	}
	entry.failures++
	entry.last = now
	c.entries[peerKey] = entry
}

func (c *p2pFailureCache) markSuccess(peerKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, peerKey)
}

var globalP2PFailureCache = newP2PFailureCache(defaultP2PFailureTTL, defaultP2PFailureThreshold)
