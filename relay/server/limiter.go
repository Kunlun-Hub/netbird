package server

import (
	"context"
	"sync"
	"time"
)

const bitsPerByte = 8

type accountRateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	lastRate map[string]int
}

type tokenBucket struct {
	rateBytesPerSecond int64
	capacityBytes      int64
	availableBytes     int64
	lastRefill         time.Time
}

func newAccountRateLimiter() *accountRateLimiter {
	return &accountRateLimiter{
		buckets:  make(map[string]*tokenBucket),
		lastRate: make(map[string]int),
	}
}

func (l *accountRateLimiter) wait(ctx context.Context, accountID string, rateLimitMbps int, bytes int) error {
	if accountID == "" || rateLimitMbps <= 0 || bytes <= 0 {
		return nil
	}
	for {
		wait := l.reserve(accountID, rateLimitMbps, int64(bytes), time.Now())
		if wait <= 0 {
			return nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *accountRateLimiter) reserve(accountID string, rateLimitMbps int, bytes int64, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket := l.buckets[accountID]
	if bucket == nil || l.lastRate[accountID] != rateLimitMbps {
		rateBytes := int64(rateLimitMbps) * 1000 * 1000 / bitsPerByte
		if rateBytes <= 0 {
			return 0
		}
		bucket = &tokenBucket{
			rateBytesPerSecond: rateBytes,
			capacityBytes:      rateBytes,
			availableBytes:     rateBytes,
			lastRefill:         now,
		}
		l.buckets[accountID] = bucket
		l.lastRate[accountID] = rateLimitMbps
	}

	elapsed := now.Sub(bucket.lastRefill)
	if elapsed > 0 {
		refill := elapsed.Nanoseconds() * bucket.rateBytesPerSecond / int64(time.Second)
		bucket.availableBytes += refill
		if bucket.availableBytes > bucket.capacityBytes {
			bucket.availableBytes = bucket.capacityBytes
		}
		bucket.lastRefill = now
	}

	if bucket.availableBytes >= bytes {
		bucket.availableBytes -= bytes
		return 0
	}
	needed := bytes - bucket.availableBytes
	bucket.availableBytes = 0
	waitNanos := needed * int64(time.Second) / bucket.rateBytesPerSecond
	if waitNanos <= 0 {
		return time.Nanosecond
	}
	return time.Duration(waitNanos)
}
