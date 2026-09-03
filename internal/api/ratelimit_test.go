package api

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsBurstThenDenies(t *testing.T) {
	moment := time.Unix(1_770_000_000, 0)
	limiter := newRateLimiter(10, 3, func() time.Time { return moment })

	for attempt := 1; attempt <= 3; attempt++ {
		if allowed, _ := limiter.allow("k1"); !allowed {
			t.Fatalf("allow() attempt %d = false, want true within burst", attempt)
		}
	}
	allowed, retryAfter := limiter.allow("k1")
	if allowed {
		t.Fatal("allow() beyond burst = true, want false")
	}
	if retryAfter < time.Second {
		t.Fatalf("allow() retryAfter = %v, want at least 1s", retryAfter)
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	moment := time.Unix(1_770_000_000, 0)
	limiter := newRateLimiter(10, 1, func() time.Time { return moment })

	if allowed, _ := limiter.allow("k1"); !allowed {
		t.Fatal("allow() first = false")
	}
	if allowed, _ := limiter.allow("k1"); allowed {
		t.Fatal("allow() second = true, want false")
	}

	moment = moment.Add(time.Second)
	if allowed, _ := limiter.allow("k1"); !allowed {
		t.Fatal("allow() after refill = false, want true")
	}
}

func TestRateLimiterIsPerKey(t *testing.T) {
	moment := time.Unix(1_770_000_000, 0)
	limiter := newRateLimiter(10, 1, func() time.Time { return moment })

	if allowed, _ := limiter.allow("k1"); !allowed {
		t.Fatal("allow(k1) = false")
	}
	if allowed, _ := limiter.allow("k2"); !allowed {
		t.Fatal("allow(k2) = false, one key must not spend another key's budget")
	}
}

func TestRateLimiterDoesNotExceedBurstWhileIdle(t *testing.T) {
	moment := time.Unix(1_770_000_000, 0)
	limiter := newRateLimiter(10, 2, func() time.Time { return moment })

	moment = moment.Add(time.Hour)
	allowedCount := 0
	for attempt := 0; attempt < 5; attempt++ {
		if allowed, _ := limiter.allow("k1"); allowed {
			allowedCount++
		}
	}
	if allowedCount != 2 {
		t.Fatalf("allow() after a long idle = %d allowed, want burst of 2", allowedCount)
	}
}
