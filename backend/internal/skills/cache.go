// This file is basically the layer between the application layer and the redis layer.

package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/clear-route/orbit/internal/platform/storage"
)

const cacheKeyPrefix = "marketplace"

// Cache is the Redis-backed skill cache. Keys are tenant-scoped:
//
//	marketplace:{tenantID}:{org}:{repo}  →  JSON []Skill  (TTL: configured)
type Cache struct {
	redis *storage.RedisClient
	ttl   time.Duration
}

// NewCache constructs a Cache backed by the given Redis client. ttl is the
// key lifetime; pass the value from config (Marketplace.CacheTTL).
func NewCache(redis *storage.RedisClient, ttl time.Duration) *Cache {
	return &Cache{redis: redis, ttl: ttl}
}

// CacheKey returns the tenant-scoped Redis key for a repo's skill set.
func CacheKey(tenantID, org, repo string) string {
	return fmt.Sprintf("%s:%s:%s:%s", cacheKeyPrefix, tenantID, org, repo)
}

// Get returns the cached []Skill for the given tenant + repo. The second
// return value is false on a cache miss (not an error). Redis operation
// failures wrap ErrRedisUnavailable; invalid cached JSON retains its own error.
func (c *Cache) Get(ctx context.Context, tenantID, org, repo string) ([]Skill, bool, error) {
	key := CacheKey(tenantID, org, repo)
	data, err := c.redis.Client().Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("cache get %q: %w: %w", key, ErrRedisUnavailable, err)
	}

	var skills []Skill
	if err := json.Unmarshal(data, &skills); err != nil {
		return nil, false, fmt.Errorf("cache unmarshal %q: %w", key, err)
	}
	return skills, true, nil
}

// Set writes skills to Redis under the tenant-scoped key with the configured TTL.
// Redis operation failures wrap ErrRedisUnavailable.
func (c *Cache) Set(ctx context.Context, tenantID, org, repo string, skills []Skill) error {
	key := CacheKey(tenantID, org, repo)
	data, err := json.Marshal(skills)
	if err != nil {
		return fmt.Errorf("cache marshal %q: %w", key, err)
	}
	if err := c.redis.Set(ctx, key, data, c.ttl); err != nil {
		return fmt.Errorf("cache set %q: %w: %w", key, ErrRedisUnavailable, err)
	}
	return nil
}
