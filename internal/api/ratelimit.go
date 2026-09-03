package api

import (
	"math"
	"sync"
	"time"
)

// rateLimiter is a token bucket keyed by webhook key ID.
//
// It is applied after signature verification so the key cannot be spoofed into
// exhausting somebody else's budget. Bucket count is bounded by the number of
// configured keys, so the map cannot grow without limit.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64
	burst   float64
	now     func() time.Time
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// newRateLimiter builds a limiter allowing rate requests per second with the
// given burst. now may be nil, in which case time.Now is used.
func newRateLimiter(rate, burst float64, now func() time.Time) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{buckets: make(map[string]*tokenBucket), rate: rate, burst: burst, now: now}
}

// allow consumes one token for key. When it returns false the second value is
// how long to wait before a token is available, for the Retry-After header.
func (l *rateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	moment := l.now()
	bucket, seen := l.buckets[key]
	if !seen {
		bucket = &tokenBucket{tokens: l.burst, last: moment}
		l.buckets[key] = bucket
	}

	if elapsed := moment.Sub(bucket.last).Seconds(); elapsed > 0 {
		bucket.tokens = math.Min(l.burst, bucket.tokens+elapsed*l.rate)
		bucket.last = moment
	}

	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}

	wait := time.Duration(math.Ceil((1-bucket.tokens)/l.rate*float64(time.Second))) * time.Nanosecond
	if wait < time.Second {
		wait = time.Second
	}
	return false, wait
}
