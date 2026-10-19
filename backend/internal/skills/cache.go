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

// Cache is the Redis-backed skill cache. Keys are:
//
//	marketplace:{tenantID}:{sourceKey}            →  JSON []SkillEntry  (TTL: configured)
//	skill:{tenantID}:{sourceKey}:{skillSlug}      →  JSON Skill          (TTL: configured)
//
// sourceKey is MarketplaceSource.CacheKey, owner/repo@ref. Both keys are scoped
// to the tenant and the repository rather than the marketplace slug: the slug
// comes from the display name, so two tenants (or one tenant over time) can
// give the same slug to different repositories.
type Cache struct {
	redis *storage.RedisClient
	ttl   time.Duration
}

// NewCache constructs a Cache backed by the given Redis client.
func NewCache(redis *storage.RedisClient, ttl time.Duration) *Cache {
	return &Cache{redis: redis, ttl: ttl}
}

// MarketplaceCacheKey returns the Redis key for one source's skill array.
func MarketplaceCacheKey(tenantID, sourceKey string) string {
	return fmt.Sprintf("marketplace:%s:%s", tenantID, sourceKey)
}

// SkillCacheKey returns the Redis key for one skill's full content.
func SkillCacheKey(tenantID, sourceKey, skillSlug string) string {
	return fmt.Sprintf("skill:%s:%s:%s", tenantID, sourceKey, skillSlug)
}

// GetMarketplace returns the cached []SkillEntry for a marketplace.
// The second return value is false on a miss. Redis failures wrap ErrRedisUnavailable.
func (c *Cache) GetMarketplace(ctx context.Context, tenantID, sourceKey string) ([]SkillEntry, bool, error) {
	key := MarketplaceCacheKey(tenantID, sourceKey)
	data, err := c.redis.Client().Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("cache get %q: %w: %w", key, ErrRedisUnavailable, err)
	}

	var skills []SkillEntry
	if err := json.Unmarshal(data, &skills); err != nil {
		return nil, false, fmt.Errorf("cache unmarshal %q: %w", key, err)
	}
	return skills, true, nil
}

// SetMarketplace writes the skill array to Redis with the configured TTL.
func (c *Cache) SetMarketplace(ctx context.Context, tenantID, sourceKey string, skills []SkillEntry) error {
	key := MarketplaceCacheKey(tenantID, sourceKey)
	data, err := json.Marshal(skills)
	if err != nil {
		return fmt.Errorf("cache marshal %q: %w", key, err)
	}
	if err := c.redis.Set(ctx, key, data, c.ttl); err != nil {
		return fmt.Errorf("cache set %q: %w: %w", key, ErrRedisUnavailable, err)
	}
	return nil
}

// GetSkill returns the cached full Skill record for skillSlug in one source.
// The second return value is false on a miss. Redis failures wrap ErrRedisUnavailable.
func (c *Cache) GetSkill(ctx context.Context, tenantID, sourceKey, skillSlug string) (Skill, bool, error) {
	key := SkillCacheKey(tenantID, sourceKey, skillSlug)
	data, err := c.redis.Client().Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return Skill{}, false, nil
		}
		return Skill{}, false, fmt.Errorf("cache get %q: %w: %w", key, ErrRedisUnavailable, err)
	}

	var skill Skill
	if err := json.Unmarshal(data, &skill); err != nil {
		return Skill{}, false, fmt.Errorf("cache unmarshal %q: %w", key, err)
	}
	return skill, true, nil
}

// SetSkill writes a full Skill record to Redis with the configured TTL.
func (c *Cache) SetSkill(ctx context.Context, tenantID, sourceKey, skillSlug string, skill Skill) error {
	key := SkillCacheKey(tenantID, sourceKey, skillSlug)
	data, err := json.Marshal(skill)
	if err != nil {
		return fmt.Errorf("cache marshal %q: %w", key, err)
	}
	if err := c.redis.Set(ctx, key, data, c.ttl); err != nil {
		return fmt.Errorf("cache set %q: %w: %w", key, ErrRedisUnavailable, err)
	}
	return nil
}
