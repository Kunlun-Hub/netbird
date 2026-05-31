package retry

import (
	"time"

	"github.com/cenkalti/backoff/v4"
	log "github.com/sirupsen/logrus"
)

// RetryNotify runs operation with backoff and emits a structured warning before each retry.
func RetryNotify(operation backoff.Operation, backOff backoff.BackOff, component, operationName string) error {
	attempt := 0
	return backoff.RetryNotify(
		func() error {
			attempt++
			return operation()
		},
		backOff,
		func(err error, nextDelay time.Duration) {
			log.WithFields(log.Fields{
				"component":      component,
				"operation":      operationName,
				"attempt":        attempt,
				"classification": Classify(err).String(),
				"next_delay":     nextDelay,
			}).WithError(err).Warn("retrying reconnect operation")
		},
	)
}
