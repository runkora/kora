package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/redis/go-redis/v9"
)

const budgetKeyTTL = 35 * 24 * time.Hour

// SpendCounter tracks per-principal monthly spend in Redis.
// Both methods fail open: errors are returned so callers can log and continue.
type SpendCounter interface {
	// GetSpend returns the summed current-period spend across all applicable
	// budget categories for the principal. On a cache miss it rehydrates
	// from the usage repository and primes Redis.
	GetSpend(ctx context.Context, tenantID, principalID string, categories []policy.BudgetCategory) (float64, error)

	// IncrementSpend atomically adds amount to each applicable category counter.
	IncrementSpend(ctx context.Context, tenantID, principalID string, categories []policy.BudgetCategory, amount float64) error
}

// redisOps is the narrow set of Redis commands used by RedisBudgetCounter.
type redisOps interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	MGet(ctx context.Context, keys ...string) *redis.SliceCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	IncrByFloat(ctx context.Context, key string, value float64) *redis.FloatCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
}

// RedisBudgetCounter implements SpendCounter using Redis INCRBYFLOAT.
//
// Per-category key format:
//
//	budget:{principal_id}:role:{role_name}:{period}
//	budget:{principal_id}:grant:{grant_id}:{period}
//
// Period format is cadence-dependent: "2026-06" (monthly), "2026-Q2" (quarterly),
// "2026" (yearly). TenantID is absent: principal IDs are UUIDs globally unique
// across tenants, so cross-tenant collision is impossible.
//
// When categories is nil/empty (principal has no configured budget), both GetSpend
// and IncrementSpend fall back to the legacy single-key format
// budget:{principal_id}:{YYYY-MM} so that enforcement and increment still work
// for principals whose bundle entry hasn't been compiled yet.
type RedisBudgetCounter struct {
	rdb    redisOps
	usage  InferenceUsageRepository
	logger logging.Logger
}

// NewRedisBudgetCounter constructs a RedisBudgetCounter.
func NewRedisBudgetCounter(rc *storage.RedisClient, usage InferenceUsageRepository, logger logging.Logger) *RedisBudgetCounter {
	return &RedisBudgetCounter{rdb: rc.Client(), usage: usage, logger: logger}
}

// categoryKey builds the per-category Redis key for a single budget assignment.
func categoryKey(principalID string, cat policy.BudgetCategory, t time.Time) string {
	period := periodForCadence(cat.ResetCadence, t)
	return fmt.Sprintf("budget:%s:%s:%s:%s", principalID, cat.Source, cat.ID, period)
}

// legacyBudgetKey is the pre-redesign single-key format, used as fallback.
func legacyBudgetKey(principalID string, t time.Time) string {
	return fmt.Sprintf("budget:%s:%s", principalID, t.UTC().Format("2006-01"))
}

func periodForCadence(cadence string, t time.Time) string {
	u := t.UTC()
	switch cadence {
	case "quarterly":
		q := (int(u.Month())-1)/3 + 1
		return fmt.Sprintf("%d-Q%d", u.Year(), q)
	case "yearly":
		return fmt.Sprintf("%d", u.Year())
	default: // "monthly" and anything unrecognized
		return u.Format("2006-01")
	}
}

// GetSpend returns the summed current-period spend across all applicable budget
// categories. When categories is nil/empty it falls back to the legacy single key.
// On a cache miss it rehydrates the aggregate from the usage repository (the DB
// is authoritative; Redis is a fast read layer).
func (c *RedisBudgetCounter) GetSpend(ctx context.Context, tenantID, principalID string, categories []policy.BudgetCategory) (float64, error) {
	now := time.Now().UTC()

	if len(categories) == 0 {
		return c.getLegacySpend(ctx, tenantID, principalID, now)
	}

	keys := make([]string, len(categories))
	for i, cat := range categories {
		keys[i] = categoryKey(principalID, cat, now)
	}

	vals, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return 0, fmt.Errorf("budget counter mget: %w", err)
	}

	var total float64
	var misses []int
	for i, v := range vals {
		if v == nil {
			misses = append(misses, i)
			continue
		}
		s, ok := v.(string)
		if !ok {
			misses = append(misses, i)
			continue
		}
		f, parseErr := strconv.ParseFloat(s, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("budget counter parse %q: %w", keys[i], parseErr)
		}
		total += f
	}

	// Rehydrate any missing category keys from the DB.
	// One DB call covers all misses. The DB stores total spend across all categories,
	// so the miss keys collectively contribute max(0, dbSpend - hitTotal) to avoid
	// double-counting values already tracked in warm Redis keys.
	// The residual is attributed to the first miss key; remaining miss keys (new
	// categories added mid-period) are seeded to 0.
	if len(misses) > 0 {
		dbSpend, dbErr := c.usage.GetMonthlySpend(ctx, tenantID, principalID, now)
		if dbErr != nil {
			return 0, fmt.Errorf("budget counter rehydrate: %w", dbErr)
		}
		residual := dbSpend - total
		if residual < 0 {
			residual = 0
		}
		total += residual
		if setErr := c.rdb.Set(ctx, keys[misses[0]], residual, budgetKeyTTL).Err(); setErr != nil {
			c.logger.Warn(ctx, "budget counter set failed during rehydration",
				logging.Field{Key: "key", Value: keys[misses[0]]},
				logging.Field{Key: "error", Value: setErr.Error()},
			)
		}
		for _, idx := range misses[1:] {
			if setErr := c.rdb.Set(ctx, keys[idx], 0, budgetKeyTTL).Err(); setErr != nil {
				c.logger.Warn(ctx, "budget counter set failed (zero seed)",
					logging.Field{Key: "key", Value: keys[idx]},
					logging.Field{Key: "error", Value: setErr.Error()},
				)
			}
		}
	}

	return total, nil
}

func (c *RedisBudgetCounter) getLegacySpend(ctx context.Context, tenantID, principalID string, now time.Time) (float64, error) {
	key := legacyBudgetKey(principalID, now)
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

	total, dbErr := c.usage.GetMonthlySpend(ctx, tenantID, principalID, now)
	if dbErr != nil {
		return 0, fmt.Errorf("budget counter rehydrate: %w", dbErr)
	}
	if setErr := c.rdb.Set(ctx, key, total, budgetKeyTTL).Err(); setErr != nil {
		c.logger.Warn(ctx, "budget counter set failed during rehydration",
			logging.Field{Key: "key", Value: key},
			logging.Field{Key: "error", Value: setErr.Error()},
		)
	}
	return total, nil
}

// IncrementSpend atomically increments each applicable category counter and
// ensures each key carries a TTL. When categories is nil/empty it increments
// the legacy single key.
func (c *RedisBudgetCounter) IncrementSpend(ctx context.Context, tenantID, principalID string, categories []policy.BudgetCategory, amount float64) error {
	now := time.Now().UTC()

	if len(categories) == 0 {
		return c.incrementLegacy(ctx, principalID, now, amount)
	}

	for _, cat := range categories {
		key := categoryKey(principalID, cat, now)
		if err := c.incrementKey(ctx, key, amount); err != nil {
			return err
		}
	}
	return nil
}

func (c *RedisBudgetCounter) incrementLegacy(ctx context.Context, principalID string, now time.Time, amount float64) error {
	return c.incrementKey(ctx, legacyBudgetKey(principalID, now), amount)
}

func (c *RedisBudgetCounter) incrementKey(ctx context.Context, key string, amount float64) error {
	if _, err := c.rdb.IncrByFloat(ctx, key, amount).Result(); err != nil {
		return fmt.Errorf("budget counter incr %q: %w", key, err)
	}

	// Refresh TTL on every write (35 days from last write, per ADR).
	if expErr := c.rdb.Expire(ctx, key, budgetKeyTTL).Err(); expErr != nil {
		c.logger.Warn(ctx, "budget counter expire failed",
			logging.Field{Key: "key", Value: key},
			logging.Field{Key: "error", Value: expErr.Error()},
		)
	}
	return nil
}
