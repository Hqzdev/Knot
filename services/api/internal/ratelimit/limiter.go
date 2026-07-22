package ratelimit

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter interface {
	Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error)
}

type memoryEntry struct {
	count     int64
	expiresAt time.Time
}

type MemoryLimiter struct {
	mu      sync.Mutex
	entries map[string]memoryEntry
	now     func() time.Time
}

func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{entries: make(map[string]memoryEntry), now: time.Now}
}

func (limiter *MemoryLimiter) Allow(_ context.Context, key string, limit int64, window time.Duration) (bool, error) {
	if key == "" || limit <= 0 || window <= 0 {
		return false, errors.New("invalid rate limit")
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now().UTC()
	entry := limiter.entries[key]
	if !entry.expiresAt.After(now) {
		entry = memoryEntry{expiresAt: now.Add(window)}
	}
	entry.count++
	limiter.entries[key] = entry
	if len(limiter.entries) > 4096 {
		for entryKey, candidate := range limiter.entries {
			if !candidate.expiresAt.After(now) {
				delete(limiter.entries, entryKey)
			}
		}
	}
	return entry.count <= limit, nil
}

type RedisLimiter struct {
	client *redis.Client
	script *redis.Script
}

func NewRedisLimiter(client *redis.Client) (*RedisLimiter, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	return &RedisLimiter{
		client: client,
		script: redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return count
`),
	}, nil
}

func (limiter *RedisLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error) {
	if key == "" || limit <= 0 || window <= 0 {
		return false, errors.New("invalid rate limit")
	}
	count, err := limiter.script.Run(ctx, limiter.client, []string{"knot:rate:" + key}, window.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return count <= limit, nil
}
