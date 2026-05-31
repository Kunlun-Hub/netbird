package retry

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// Policy describes an exponential backoff profile used by reconnect loops.
type Policy struct {
	InitialInterval     time.Duration
	RandomizationFactor float64
	Multiplier          float64
	MaxInterval         time.Duration
	MaxElapsedTime      time.Duration
}

var (
	// ClientSessionPolicy wraps the full client connect/login/engine startup cycle.
	ClientSessionPolicy = Policy{
		InitialInterval:     time.Second,
		RandomizationFactor: 1,
		Multiplier:          1.7,
		MaxInterval:         15 * time.Second,
		MaxElapsedTime:      3 * 30 * 24 * time.Hour,
	}

	// ControlStreamPolicy is used by long-lived Management and Signal streams.
	ControlStreamPolicy = Policy{
		InitialInterval:     800 * time.Millisecond,
		RandomizationFactor: 1,
		Multiplier:          1.7,
		MaxInterval:         10 * time.Second,
		MaxElapsedTime:      3 * 30 * 24 * time.Hour,
	}

	// RelayReconnectPolicy is used after the fast same-server Relay reconnect attempt fails.
	RelayReconnectPolicy = Policy{
		InitialInterval:     2 * time.Second,
		RandomizationFactor: 0,
		Multiplier:          2,
		MaxInterval:         60 * time.Second,
		MaxElapsedTime:      0,
	}

	// PeerInitialPolicy gives a peer time to establish its first connection.
	PeerInitialPolicy = Policy{
		InitialInterval:     3 * time.Second,
		RandomizationFactor: 0.1,
		Multiplier:          2,
		MaxInterval:         60 * time.Second,
		MaxElapsedTime:      0,
	}

	// PeerReconnectPolicy is used after connection-state changes trigger fresh peer negotiation.
	PeerReconnectPolicy = Policy{
		InitialInterval:     800 * time.Millisecond,
		RandomizationFactor: 0.1,
		Multiplier:          2,
		MaxInterval:         60 * time.Second,
		MaxElapsedTime:      0,
	}
)

func (p Policy) WithMaxInterval(maxInterval time.Duration) Policy {
	p.MaxInterval = maxInterval
	return p
}

// NewBackOff builds a context-aware exponential backoff from a named policy.
func NewBackOff(ctx context.Context, policy Policy) backoff.BackOff {
	return backoff.WithContext(&backoff.ExponentialBackOff{
		InitialInterval:     policy.InitialInterval,
		RandomizationFactor: policy.RandomizationFactor,
		Multiplier:          policy.Multiplier,
		MaxInterval:         policy.MaxInterval,
		MaxElapsedTime:      policy.MaxElapsedTime,
		Stop:                backoff.Stop,
		Clock:               backoff.SystemClock,
	}, ctx)
}
