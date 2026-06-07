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

const quotaKeyTTL = 35 * 24 * time.Hour

// SpendCounter tracks per-principal monthly spend in Redis.
// Both methods fail open: errors are returned so callers can log and continue.
type SpendCounter interface {
	// GetSpend returns the current-month spend for the principal.
	// On a cache miss it rehydrates from the usage repository and primes Redis.
	GetSpend(ctx context.Context, tenantID, principalID string) (float64, error)

	// IncrementSpend atomically adds amount to the principal's current-month counter.
	IncrementSpend(ctx context.Context, tenantID, principalID string, amount float64) error
}

// RedisQuotaCounter implements SpendCounter using Redis INCRBYFLOAT.
// Key format: quota:{principal_id}:{YYYY-MM}
type RedisQuotaCounter struct {
	rc     *storage.RedisClient
	usage  InferenceUsageRepository
	logger logging.Logger
}

// NewRedisQuotaCounter constructs a RedisQuotaCounter.
func NewRedisQuotaCounter(rc *storage.RedisClient, usage InferenceUsageRepository, logger logging.Logger) *RedisQuotaCounter {
	return &RedisQuotaCounter{rc: rc, usage: usage, logger: logger}
}

func quotaKey(principalID string, t time.Time) string {
	return fmt.Sprintf("quota:%s:%s", principalID, t.UTC().Format("2006-01"))
}

// GetSpend returns the current-month spend for the principal.
// On a Redis cache miss it queries the usage repository to rehydrate the counter.
func (c *RedisQuotaCounter) GetSpend(ctx context.Context, tenantID, principalID string) (float64, error) {
	now := time.Now().UTC()
	key := quotaKey(principalID, now)

	raw, err := c.rc.Client().Get(ctx, key).Result()
	if err == nil {
		v, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("quota counter parse %q: %w", key, parseErr)
		}
		return v, nil
	}
	if !errors.Is(err, redis.Nil) {
		return 0, fmt.Errorf("quota counter get %q: %w", key, err)
	}

	// Cold miss — rehydrate from DB.
	total, dbErr := c.usage.GetMonthlySpend(ctx, tenantID, principalID, now)
	if dbErr != nil {
		return 0, fmt.Errorf("quota counter rehydrate: %w", dbErr)
	}

	// Prime the Redis key (best-effort — ignore error so we still return the value).
	if setErr := c.rc.Client().Set(ctx, key, total, quotaKeyTTL).Err(); setErr != nil {
		c.logger.Warn(ctx, "quota counter set failed during rehydration",
			logging.Field{Key: "key", Value: key},
			logging.Field{Key: "error", Value: setErr.Error()},
		)
	}

	return total, nil
}

// IncrementSpend atomically increments the principal's current-month counter
// and ensures the key carries a 35-day TTL.
func (c *RedisQuotaCounter) IncrementSpend(ctx context.Context, tenantID, principalID string, amount float64) error {
	now := time.Now().UTC()
	key := quotaKey(principalID, now)

	if _, err := c.rc.Client().IncrByFloat(ctx, key, amount).Result(); err != nil {
		return fmt.Errorf("quota counter incr %q: %w", key, err)
	}

	// Set TTL only when the key has none (freshly created by INCRBYFLOAT).
	ttl, err := c.rc.Client().TTL(ctx, key).Result()
	if err != nil {
		// Non-fatal: the counter was incremented; TTL failure is best-effort.
		return nil
	}
	if ttl < 0 {
		c.rc.Client().Expire(ctx, key, quotaKeyTTL)
	}

	return nil
}
