package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/clear-route/orbit/internal/platform/storage"
)

// Cache is the Redis skill cache. Every key carries the tenant ID:
//
//	skills:{tenant}:{slug}:list               skillList
//	skills:{tenant}:{slug}:failure            crawlFailure (failure TTL)
//	skills:{tenant}:{slug}:file:{sha}:{path}  file content
type Cache struct {
	redis      *storage.RedisClient
	ttl        time.Duration
	failureTTL time.Duration
}

// NewCache returns a Cache whose entries expire after ttl, and failed crawls
// after failureTTL.
func NewCache(redis *storage.RedisClient, ttl, failureTTL time.Duration) *Cache {
	return &Cache{redis: redis, ttl: ttl, failureTTL: failureTTL}
}

func cacheKey(m *Marketplace, parts ...string) string {
	var key strings.Builder
	key.WriteString("skills:" + m.TenantID + ":" + m.Slug)
	for _, p := range parts {
		key.WriteString(":" + p)
	}
	return key.String()
}

// GetList returns the cached crawl of m, if any.
func (c *Cache) GetList(ctx context.Context, m *Marketplace) (skillList, bool, error) {
	var list skillList
	ok, err := c.get(ctx, cacheKey(m, "list"), &list)
	return list, ok, err
}

// SetList caches a crawl of m.
func (c *Cache) SetList(ctx context.Context, m *Marketplace, list skillList) error {
	return c.set(ctx, cacheKey(m, "list"), list, c.ttl)
}

// GetFailure returns the cached failed crawl of m, if any.
func (c *Cache) GetFailure(ctx context.Context, m *Marketplace) (crawlFailure, bool, error) {
	var f crawlFailure
	ok, err := c.get(ctx, cacheKey(m, "failure"), &f)
	return f, ok, err
}

// SetFailure caches a failed crawl of m for the failure TTL.
func (c *Cache) SetFailure(ctx context.Context, m *Marketplace, f crawlFailure) error {
	return c.set(ctx, cacheKey(m, "failure"), f, c.failureTTL)
}

// GetFile returns the cached content of one skill file at sha.
func (c *Cache) GetFile(ctx context.Context, m *Marketplace, sha, path string) (string, bool, error) {
	var content string
	ok, err := c.get(ctx, cacheKey(m, "file", sha, path), &content)
	return content, ok, err
}

// SetFile caches the content of one skill file at sha.
func (c *Cache) SetFile(ctx context.Context, m *Marketplace, sha, path, content string) error {
	return c.set(ctx, cacheKey(m, "file", sha, path), content, c.ttl)
}

func (c *Cache) get(ctx context.Context, key string, dest any) (bool, error) {
	data, err := c.redis.Client().Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cache get %q: %w: %w", key, storage.ErrRedisUnavailable, err)
	}
	// An entry in an older shape is a miss and is rewritten.
	if err := json.Unmarshal(data, dest); err != nil {
		return false, nil
	}
	return true, nil
}

func (c *Cache) set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache encode %q: %w", key, err)
	}
	if err := c.redis.Set(ctx, key, data, ttl); err != nil {
		return fmt.Errorf("cache set %q: %w: %w", key, storage.ErrRedisUnavailable, err)
	}
	return nil
}
