package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/redis/go-redis/v9"
)

const budgetKeyTTL = 35 * 24 * time.Hour

// SpendCounter tracks per-principal monthly spend in Redis.
// All methods fail open: errors are returned so callers can log and continue.
type SpendCounter interface {
	// GetSpend returns the principal's canonical current-period spend.
	// On a cache miss it rehydrates from the usage repository and primes Redis.
	GetSpend(ctx context.Context, tenantID, principalID string) (float64, error)

	// IncrementSpend adds amount to the canonical counter and to each
	// applicable per-category attribution counter.
	IncrementSpend(ctx context.Context, tenantID, principalID string, categories []policy.BudgetCategory, amount float64) error

	// GetCategorySpends returns the attributed spend per category, aligned to
	// the input slice. Missing counters read as 0 (a category with no traffic
	// this period has no key).
	GetCategorySpends(ctx context.Context, principalID string, categories []policy.BudgetCategory) ([]float64, error)

	// ListActiveRoleAttributions returns principal ID → role names that have a
	// role attribution counter for the current monthly period. This is how the
	// budgets listing discovers which principals hold budgeted OIDC roles: role
	// membership is IDP-asserted per request, so the attribution counters written
	// on inference traffic are the only durable record of it. Principals with no
	// requests this period are absent.
	ListActiveRoleAttributions(ctx context.Context) (map[string][]string, error)
}

// redisOps is the narrow set of Redis commands used by RedisBudgetCounter.
type redisOps interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	MGet(ctx context.Context, keys ...string) *redis.SliceCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	IncrByFloat(ctx context.Context, key string, value float64) *redis.FloatCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
	Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd
}

// RedisBudgetCounter implements SpendCounter using Redis INCRBYFLOAT.
//
// Key layout:
//
//	budget:{principal_id}:{period}                      canonical — read for enforcement, always written
//	budget:{principal_id}:role:{role_name}:{period}     attribution only — written, never used for enforcement
//	budget:{principal_id}:grant:{grant_id}:{period}     attribution only
//
// The canonical counter holds the principal's full period spend and is the
// single value compared against the effective budget limit. Category counters
// each also carry the full spend of the requests made while that category
// applied — they exist for per-source reporting, never for enforcement
// arithmetic (summing them would multiply-count spend, since every category
// is incremented on every request).
//
// Period format is cadence-dependent: "2026-06" (monthly), "2026-Q2"
// (quarterly), "2026" (yearly). The canonical counter is always monthly —
// non-monthly cadences are reserved and not yet enforced. TenantID is absent:
// principal IDs are UUIDs globally unique across tenants, so cross-tenant
// collision is impossible.
type RedisBudgetCounter struct {
	rdb    redisOps
	usage  InferenceUsageRepository
	logger logging.Logger
}

// NewRedisBudgetCounter constructs a RedisBudgetCounter.
func NewRedisBudgetCounter(rc *storage.RedisClient, usage InferenceUsageRepository, logger logging.Logger) *RedisBudgetCounter {
	return &RedisBudgetCounter{rdb: rc.Client(), usage: usage, logger: logger}
}

// canonicalBudgetKey is the per-principal enforcement counter key.
func canonicalBudgetKey(principalID string, t time.Time) string {
	return fmt.Sprintf("budget:%s:%s", principalID, t.UTC().Format("2006-01"))
}

// categoryKey builds the per-category attribution key for a single budget assignment.
func categoryKey(principalID string, cat policy.BudgetCategory, t time.Time) string {
	period := periodForCadence(cat.ResetCadence, t)
	return fmt.Sprintf("budget:%s:%s:%s:%s", principalID, cat.Source, cat.ID, period)
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

// GetSpend returns the canonical current-period spend for the principal.
// On a cache miss it rehydrates from the usage repository (the DB is
// authoritative; Redis is a fast read layer).
func (c *RedisBudgetCounter) GetSpend(ctx context.Context, tenantID, principalID string) (float64, error) {
	now := time.Now().UTC()
	key := canonicalBudgetKey(principalID, now)

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

// GetCategorySpends returns the attributed spend for each category, aligned to
// the input order. Missing keys read as 0.
func (c *RedisBudgetCounter) GetCategorySpends(ctx context.Context, principalID string, categories []policy.BudgetCategory) ([]float64, error) {
	if len(categories) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	keys := make([]string, len(categories))
	for i, cat := range categories {
		keys[i] = categoryKey(principalID, cat, now)
	}

	vals, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("budget counter mget: %w", err)
	}

	spends := make([]float64, len(categories))
	for i, v := range vals {
		s, ok := v.(string)
		if !ok {
			continue // nil (missing key) or unexpected type → 0
		}
		f, parseErr := strconv.ParseFloat(s, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("budget counter parse %q: %w", keys[i], parseErr)
		}
		spends[i] = f
	}
	return spends, nil
}

// ListActiveRoleAttributions scans for role attribution counters in the
// current monthly period and returns principal ID → role names. The scan is
// O(budget keyspace) — keys ≈ principals × categories — which is acceptable at
// current scale; revisit if key cardinality grows.
func (c *RedisBudgetCounter) ListActiveRoleAttributions(ctx context.Context) (map[string][]string, error) {
	period := time.Now().UTC().Format("2006-01")
	pattern := "budget:*:role:*:" + period

	out := make(map[string][]string)
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return nil, fmt.Errorf("budget counter scan: %w", err)
		}
		for _, k := range keys {
			// budget:{principal_id}:role:{role_name}:{period}
			trimmed := strings.TrimPrefix(k, "budget:")
			trimmed = strings.TrimSuffix(trimmed, ":"+period)
			parts := strings.SplitN(trimmed, ":role:", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				continue
			}
			out[parts[0]] = append(out[parts[0]], parts[1])
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return out, nil
}

// IncrementSpend increments the canonical counter and each applicable
// per-category attribution counter, ensuring every key carries a TTL.
func (c *RedisBudgetCounter) IncrementSpend(ctx context.Context, tenantID, principalID string, categories []policy.BudgetCategory, amount float64) error {
	now := time.Now().UTC()

	if err := c.incrementKey(ctx, canonicalBudgetKey(principalID, now), amount); err != nil {
		return err
	}

	for _, cat := range categories {
		key := categoryKey(principalID, cat, now)
		if err := c.incrementKey(ctx, key, amount); err != nil {
			return err
		}
	}
	return nil
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
