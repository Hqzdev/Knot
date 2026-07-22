package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLimiterResetsExpiredWindows(t *testing.T) {
	limiter := NewMemoryLimiter()
	now := time.Unix(100, 0).UTC()
	limiter.now = func() time.Time { return now }
	for attempt := 0; attempt < 2; attempt++ {
		allowed, err := limiter.Allow(context.Background(), "login:client", 2, time.Minute)
		if err != nil || !allowed {
			t.Fatalf("expected attempt %d to pass: %v", attempt, err)
		}
	}
	allowed, err := limiter.Allow(context.Background(), "login:client", 2, time.Minute)
	if err != nil || allowed {
		t.Fatalf("expected rate limit rejection: %v", err)
	}
	now = now.Add(time.Minute)
	allowed, err = limiter.Allow(context.Background(), "login:client", 2, time.Minute)
	if err != nil || !allowed {
		t.Fatalf("expected reset window: %v", err)
	}
}
