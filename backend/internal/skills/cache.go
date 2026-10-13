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
//	marketplace:{tenantID}:{marketplaceSlug}  →  JSON []SkillEntry  (TTL: configured)
//	{skillSlug}                               →  JSON Skill          (TTL: configured)
//
// Marketplace keys are tenant-scoped so two tenants with access to the same
// repository get independent cache entries. Skill keys use the skillSlug
// directly (which already encodes the marketplace) and are shared across
// tenants; tenant access is enforced at the service layer before Redis is read.
type Cache struct {
	redis *storage.RedisClient
	ttl   time.Duration
}

// NewCache constructs a Cache backed by the given Redis client.
func NewCache(redis *storage.RedisClient, ttl time.Duration) *Cache {
	return &Cache{redis: redis, ttl: ttl}
}

// MarketplaceCacheKey returns the Redis key for a marketplace's skill array,
// scoped to the tenant so two tenants with access to the same repo get
// independent cache entries.
func MarketplaceCacheKey(tenantID, marketplaceSlug string) string {
	return fmt.Sprintf("marketplace:%s:%s", tenantID, marketplaceSlug)
}

// SkillCacheKey returns the Redis key for one skill's full content.
// The skillSlug already encodes the marketplace, so no additional prefix is needed.
func SkillCacheKey(skillSlug string) string {
	return skillSlug
}

// GetMarketplace returns the cached []SkillEntry for a marketplace.
// The second return value is false on a miss. Redis failures wrap ErrRedisUnavailable.
func (c *Cache) GetMarketplace(ctx context.Context, tenantID, marketplaceSlug string) ([]SkillEntry, bool, error) {
	key := MarketplaceCacheKey(tenantID, marketplaceSlug)
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
func (c *Cache) SetMarketplace(ctx context.Context, tenantID, marketplaceSlug string, skills []SkillEntry) error {
	key := MarketplaceCacheKey(tenantID, marketplaceSlug)
	data, err := json.Marshal(skills)
	if err != nil {
		return fmt.Errorf("cache marshal %q: %w", key, err)
	}
	if err := c.redis.Set(ctx, key, data, c.ttl); err != nil {
		return fmt.Errorf("cache set %q: %w: %w", key, ErrRedisUnavailable, err)
	}
	return nil
}

// GetSkill returns the cached full Skill record for a skillSlug.
// The second return value is false on a miss. Redis failures wrap ErrRedisUnavailable.
func (c *Cache) GetSkill(ctx context.Context, skillSlug string) (Skill, bool, error) {
	key := SkillCacheKey(skillSlug)
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
func (c *Cache) SetSkill(ctx context.Context, skillSlug string, skill Skill) error {
	key := SkillCacheKey(skillSlug)
	data, err := json.Marshal(skill)
	if err != nil {
		return fmt.Errorf("cache marshal %q: %w", key, err)
	}
	if err := c.redis.Set(ctx, key, data, c.ttl); err != nil {
		return fmt.Errorf("cache set %q: %w: %w", key, ErrRedisUnavailable, err)
	}
	return nil
}
