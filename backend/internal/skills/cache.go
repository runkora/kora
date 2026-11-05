package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/runkora/kora/backend/internal/platform/storage"
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
	// fileBudget caps the bytes of supporting files cached per Marketplace and commit. Zero is no cap.
	fileBudget int64
}

// NewCache returns a Cache whose entries expire after ttl, failed crawls after
// failureTTL, and that caches at most fileBudget bytes of supporting files per
// Marketplace and commit.
func NewCache(redis *storage.RedisClient, ttl, failureTTL time.Duration, fileBudget int64) *Cache {
	return &Cache{redis: redis, ttl: ttl, failureTTL: failureTTL, fileBudget: fileBudget}
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

// SetList caches a crawl of m. The list outlives its freshness by
// listRetention, so an expired list can be kept when its commit has not
// changed.
func (c *Cache) SetList(ctx context.Context, m *Marketplace, list skillList) error {
	return c.set(ctx, cacheKey(m, "list"), list, listRetention*c.ttl)
}

// listRetention is how many cache TTLs a list is kept for.
const listRetention = 2

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

// SetFile caches the content of one skill file at sha. A file that would take
// the Marketplace past fileBudget, counted in stored bytes, is not cached, so one
// Marketplace cannot fill the shared Redis. Files expire with the budget counter.
func (c *Cache) SetFile(ctx context.Context, m *Marketplace, sha, path, content string) error {
	key := cacheKey(m, "file", sha, path)
	data, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("cache encode %q: %w", key, err)
	}
	ttl := c.ttl
	if c.fileBudget > 0 {
		var ok bool
		if ttl, ok, err = c.chargeFileBudget(ctx, cacheKey(m, "filebytes", sha), int64(len(data))); err != nil || !ok {
			return err
		}
	}
	if err := c.redis.Set(ctx, key, data, ttl); err != nil {
		return fmt.Errorf("cache set %q: %w: %w", key, storage.ErrRedisUnavailable, err)
	}
	return nil
}

// chargeFileBudget adds size to the budget counter at key and reports whether it
// fits, with the counter's remaining lifetime for the file entry.
func (c *Cache) chargeFileBudget(ctx context.Context, key string, size int64) (time.Duration, bool, error) {
	rc := c.redis.Client()
	used, err := rc.IncrBy(ctx, key, size).Result()
	if err != nil {
		return 0, false, fmt.Errorf("cache budget %q: %w: %w", key, storage.ErrRedisUnavailable, err)
	}
	if used > c.fileBudget {
		if err = rc.DecrBy(ctx, key, size).Err(); err != nil {
			return 0, false, fmt.Errorf("cache budget %q: %w: %w", key, storage.ErrRedisUnavailable, err)
		}
		return 0, false, nil
	}
	ttl, err := rc.PTTL(ctx, key).Result()
	if err != nil {
		return 0, false, fmt.Errorf("cache budget %q: %w: %w", key, storage.ErrRedisUnavailable, err)
	}
	if ttl <= 0 {
		if err = rc.PExpire(ctx, key, c.ttl).Err(); err != nil {
			return 0, false, fmt.Errorf("cache budget %q: %w: %w", key, storage.ErrRedisUnavailable, err)
		}
		ttl = c.ttl
	}
	return ttl, true, nil
}

func (c *Cache) get(ctx context.Context, key string, dest any) (bool, error) {
	data, err := c.redis.Client().Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cache get %q: %w: %w", key, storage.ErrRedisUnavailable, err)
	}
	// An entry that does not decode is a miss. source() rejects an older shape.
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
