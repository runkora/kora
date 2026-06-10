package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/redis/go-redis/v9"
)

const budgetKeyTTL = 35 * 24 * time.Hour

// SpendCounter tracks per-principal monthly spend in Redis.
// Both methods fail open: errors are returned so callers can log and continue.
type SpendCounter interface {
	// GetSpend returns the current-month spend for the principal.
	// On a cache miss it rehydrates from the usage repository and primes Redis.
	GetSpend(ctx context.Context, tenantID, principalID string) (float64, error)

	// IncrementSpend atomically adds amount to the principal's current-month counter.
	IncrementSpend(ctx context.Context, tenantID, principalID string, amount float64) error
}

// redisOps is the narrow set of Redis commands used by RedisBudgetCounter.
// Scoping to these five methods keeps the production type and its test double aligned.
type redisOps interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	IncrByFloat(ctx context.Context, key string, value float64) *redis.FloatCmd
	TTL(ctx context.Context, key string) *redis.DurationCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
}

// RedisBudgetCounter implements SpendCounter using Redis INCRBYFLOAT.
// Key format: budget:{principal_id}:{YYYY-MM}
// TenantID is intentionally absent from the key: principal IDs are UUIDs that
// are globally unique across tenants, so cross-tenant key collision is impossible.
type RedisBudgetCounter struct {
	rdb    redisOps
	usage  InferenceUsageRepository
	logger logging.Logger
}

// NewRedisBudgetCounter constructs a RedisBudgetCounter.
func NewRedisBudgetCounter(rc *storage.RedisClient, usage InferenceUsageRepository, logger logging.Logger) *RedisBudgetCounter {
	return &RedisBudgetCounter{rdb: rc.Client(), usage: usage, logger: logger}
}

func budgetKey(principalID string, t time.Time) string {
	return fmt.Sprintf("budget:%s:%s", principalID, t.UTC().Format("2006-01"))
}

// GetSpend returns the current-month spend for the principal.
// On a Redis cache miss it queries the usage repository to rehydrate the counter.
func (c *RedisBudgetCounter) GetSpend(ctx context.Context, tenantID, principalID string) (float64, error) {
	now := time.Now().UTC()
	key := budgetKey(principalID, now)

	raw, err := c.rdb.Get(ctx, key).Result()
	if err == nil {
		v, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("budget counter parse %q: %w", key, parseErr)
		}
		return v, nil
	}
	if !errors.Is(err, redis.Nil) {
		return 0, fmt.Errorf("budget counter get %q: %w", key, err)
	}

	// Cold miss — rehydrate from DB.
	total, dbErr := c.usage.GetMonthlySpend(ctx, tenantID, principalID, now)
	if dbErr != nil {
		return 0, fmt.Errorf("budget counter rehydrate: %w", dbErr)
	}

	// Prime the Redis key (best-effort — ignore error so we still return the value).
	if setErr := c.rdb.Set(ctx, key, total, budgetKeyTTL).Err(); setErr != nil {
		c.logger.Warn(ctx, "budget counter set failed during rehydration",
			logging.Field{Key: "key", Value: key},
			logging.Field{Key: "error", Value: setErr.Error()},
		)
	}

	return total, nil
}

// IncrementSpend atomically increments the principal's current-month counter
// and ensures the key carries a 35-day TTL.
func (c *RedisBudgetCounter) IncrementSpend(ctx context.Context, tenantID, principalID string, amount float64) error {
	now := time.Now().UTC()
	key := budgetKey(principalID, now)

	if _, err := c.rdb.IncrByFloat(ctx, key, amount).Result(); err != nil {
		return fmt.Errorf("budget counter incr %q: %w", key, err)
	}

	// Set TTL only when the key exists but has none (freshly created by INCRBYFLOAT).
	// TTL returns -1 (key exists, no TTL) or -2 (key doesn't exist). Only set
	// expiry for -1; calling Expire on a non-existent key is a no-op but misleading.
	ttl, err := c.rdb.TTL(ctx, key).Result()
	if err != nil {
		// Non-fatal: the counter was incremented; TTL failure is best-effort.
		// Log so the missing expiry is visible in observability.
		c.logger.Warn(ctx, "budget counter ttl check failed",
			logging.Field{Key: "key", Value: key},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil
	}
	if ttl == time.Duration(-1) {
		if expErr := c.rdb.Expire(ctx, key, budgetKeyTTL).Err(); expErr != nil {
			c.logger.Warn(ctx, "budget counter expire failed",
				logging.Field{Key: "key", Value: key},
				logging.Field{Key: "error", Value: expErr.Error()},
			)
		}
	}

	return nil
}
