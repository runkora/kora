package inferencegw

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/redis/go-redis/v9"
)

const (
	budgetKeyTTL = 35 * 24 * time.Hour
	// runKeyTTL outlives any plausible run; refreshed on every write so
	// long-lived runs never lose their counter mid-run.
	runKeyTTL = 7 * 24 * time.Hour

	// cadenceWeekly is the canonical string for a weekly reset window, used
	// throughout the budget counter to avoid repeating the literal.
	cadenceWeekly = "weekly"
)

// SpendScopes describes which canonical counters apply to one request — the
// product of the caller's configured ceilings (from bundle data + JWT roles)
// and the request's identity context.
type SpendScopes struct {
	TenantID    string
	PrincipalID string
	// PrincipalWindows are the windows ("weekly"/"monthly") with configured
	// principal ceilings. The monthly counter is always written on increment
	// regardless (it feeds reporting), but only listed windows are read.
	PrincipalWindows []string
	// TenantWindow is the tenant cap's window ("" = no cap configured).
	TenantWindow string
	// PoolWindows maps pooled role name → its window, for the pooled role
	// budgets the caller's JWT asserts.
	PoolWindows map[string]string
	// RunID is the request's run identity ("" = none).
	RunID string
}

// SpendCounter tracks canonical and attribution spend counters in Redis.
// All methods fail open: errors are returned so callers can log and continue.
type SpendCounter interface {
	// GetSpends reads every canonical counter named by scopes, rehydrating
	// misses from the usage repository (the DB is authoritative).
	GetSpends(ctx context.Context, scopes SpendScopes) (policy.BudgetSpends, error)

	// IncrementSpends adds amount to every applicable canonical counter
	// (principal windows, tenant, pools, run) and to each per-category
	// attribution counter.
	IncrementSpends(ctx context.Context, scopes SpendScopes, categories []policy.BudgetCategory, amount float64) error

	// GetCategorySpends returns the attributed spend per category, aligned to
	// the input slice. Missing counters read as 0.
	GetCategorySpends(ctx context.Context, principalID string, categories []policy.BudgetCategory) ([]float64, error)
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
// Key layout (see docs/adr/2026-06-10-hierarchical-budget-enforcement.md):
//
//	budget:{principal_id}:{period}                  canonical principal, per window
//	budget:tenant:{tenant_id}:{period}              tenant rollup (all principals)
//	budget:rolepool:{tenant_id}:{role}:{period}     shared role pool
//	budget:run:{run_id}                             per-run (TTL 7d, refresh on write)
//	budget:{principal_id}:{source}:{id}:{period}    attribution (never enforced)
//
// Period strings are window-specific and collision-free: "2026-06" (monthly),
// "2026-W24" (ISO week, UTC). Principal IDs are globally unique UUIDs/URIs, so
// principal keys omit the tenant; role names are tenant-scoped, so pool keys
// include it.
type RedisBudgetCounter struct {
	rdb    redisOps
	usage  InferenceUsageRepository
	logger logging.Logger
}

// NewRedisBudgetCounter constructs a RedisBudgetCounter.
func NewRedisBudgetCounter(rc *storage.RedisClient, usage InferenceUsageRepository, logger logging.Logger) *RedisBudgetCounter {
	return &RedisBudgetCounter{rdb: rc.Client(), usage: usage, logger: logger}
}

// ── periods and keys ─────────────────────────────────────────────────────────

func periodForCadence(cadence string, t time.Time) string {
	u := t.UTC()
	switch cadence {
	case cadenceWeekly:
		y, w := u.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	case "quarterly":
		q := (int(u.Month())-1)/3 + 1
		return fmt.Sprintf("%d-Q%d", u.Year(), q)
	case "yearly":
		return fmt.Sprintf("%d", u.Year())
	default: // "monthly" and anything unrecognized
		return u.Format("2006-01")
	}
}

// periodStart returns the UTC start of the window containing t.
func periodStart(window string, t time.Time) time.Time {
	u := t.UTC()
	if window == cadenceWeekly {
		// ISO week starts Monday. Weekday() is 0=Sunday … 6=Saturday.
		offset := (int(u.Weekday()) + 6) % 7
		day := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
		return day.AddDate(0, 0, -offset)
	}
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func principalKey(principalID, window string, t time.Time) string {
	return fmt.Sprintf("budget:%s:%s", principalID, periodForCadence(window, t))
}

func tenantKey(tenantID, window string, t time.Time) string {
	return fmt.Sprintf("budget:tenant:%s:%s", tenantID, periodForCadence(window, t))
}

func poolKey(tenantID, role, window string, t time.Time) string {
	return fmt.Sprintf("budget:rolepool:%s:%s:%s", tenantID, role, periodForCadence(window, t))
}

func runKey(runID string) string {
	return fmt.Sprintf("budget:run:%s", runID)
}

// categoryKey builds the per-category attribution key for a single budget assignment.
func categoryKey(principalID string, cat policy.BudgetCategory, t time.Time) string {
	period := periodForCadence(cat.ResetCadence, t)
	return fmt.Sprintf("budget:%s:%s:%s:%s", principalID, cat.Source, cat.ID, period)
}

// ── reads ────────────────────────────────────────────────────────────────────

// spendRead pairs a Redis key with the rehydration query for a miss.
type spendRead struct {
	key       string
	rehydrate func(ctx context.Context) (float64, error)
	assign    func(spends *policy.BudgetSpends, v float64)
}

// GetSpends reads every canonical counter named by scopes in one MGET,
// rehydrating misses from the usage repository and priming Redis.
func (c *RedisBudgetCounter) GetSpends(ctx context.Context, scopes SpendScopes) (policy.BudgetSpends, error) {
	now := time.Now().UTC()
	spends := policy.BudgetSpends{
		Principal: map[string]float64{},
		RolePools: map[string]float64{},
		Tenant:    map[string]float64{},
	}

	var reads []spendRead
	for _, w := range scopes.PrincipalWindows {
		window := w
		reads = append(reads, spendRead{
			key: principalKey(scopes.PrincipalID, window, now),
			rehydrate: func(ctx context.Context) (float64, error) {
				return c.usage.GetSpendSince(ctx, scopes.TenantID, scopes.PrincipalID, periodStart(window, now))
			},
			assign: func(s *policy.BudgetSpends, v float64) { s.Principal[window] = v },
		})
	}
	if scopes.TenantWindow != "" {
		window := scopes.TenantWindow
		reads = append(reads, spendRead{
			key: tenantKey(scopes.TenantID, window, now),
			rehydrate: func(ctx context.Context) (float64, error) {
				return c.usage.GetTenantSpendSince(ctx, scopes.TenantID, periodStart(window, now))
			},
			assign: func(s *policy.BudgetSpends, v float64) { s.Tenant[window] = v },
		})
	}
	for poolRole, w := range scopes.PoolWindows {
		role, window := poolRole, w
		reads = append(reads, spendRead{
			key: poolKey(scopes.TenantID, role, window, now),
			rehydrate: func(ctx context.Context) (float64, error) {
				return c.usage.GetPoolSpendSince(ctx, scopes.TenantID, role, periodStart(window, now))
			},
			assign: func(s *policy.BudgetSpends, v float64) { s.RolePools[role] = v },
		})
	}
	if scopes.RunID != "" {
		reads = append(reads, spendRead{
			key: runKey(scopes.RunID),
			rehydrate: func(ctx context.Context) (float64, error) {
				return c.usage.GetRunSpend(ctx, scopes.TenantID, scopes.RunID)
			},
			assign: func(s *policy.BudgetSpends, v float64) { s.Run = v },
		})
	}

	if len(reads) == 0 {
		return spends, nil
	}

	keys := make([]string, len(reads))
	for i, r := range reads {
		keys[i] = r.key
	}
	vals, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return spends, fmt.Errorf("budget counter mget: %w", err)
	}

	for i, r := range reads {
		var v float64
		if s, ok := vals[i].(string); ok {
			parsed, parseErr := strconv.ParseFloat(s, 64)
			if parseErr != nil {
				return spends, fmt.Errorf("budget counter parse %q: %w", r.key, parseErr)
			}
			v = parsed
		} else {
			// Miss: the DB is authoritative — rehydrate and prime the key.
			v, err = r.rehydrate(ctx)
			if err != nil {
				return spends, fmt.Errorf("budget counter rehydrate %q: %w", r.key, err)
			}
			ttl := budgetKeyTTL
			if r.key == runKey(scopes.RunID) {
				ttl = runKeyTTL
			}
			if setErr := c.rdb.Set(ctx, r.key, v, ttl).Err(); setErr != nil {
				c.logger.Warn(ctx, "budget counter set failed during rehydration",
					logging.Field{Key: "key", Value: r.key},
					logging.Field{Key: "error", Value: setErr.Error()},
				)
			}
		}
		r.assign(&spends, v)
	}

	return spends, nil
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

// ── writes ───────────────────────────────────────────────────────────────────

// IncrementSpends adds amount to every applicable canonical counter and the
// attribution counters, ensuring every key carries a TTL. The monthly
// principal counter is always written so reporting and late-added budgets
// have a warm baseline.
func (c *RedisBudgetCounter) IncrementSpends(ctx context.Context, scopes SpendScopes, categories []policy.BudgetCategory, amount float64) error {
	now := time.Now().UTC()

	windows := map[string]struct{}{"monthly": {}}
	for _, w := range scopes.PrincipalWindows {
		windows[w] = struct{}{}
	}
	for w := range windows {
		if err := c.incrementKey(ctx, principalKey(scopes.PrincipalID, w, now), amount, budgetKeyTTL); err != nil {
			return err
		}
	}

	if scopes.TenantWindow != "" {
		if err := c.incrementKey(ctx, tenantKey(scopes.TenantID, scopes.TenantWindow, now), amount, budgetKeyTTL); err != nil {
			return err
		}
	}

	for role, w := range scopes.PoolWindows {
		if err := c.incrementKey(ctx, poolKey(scopes.TenantID, role, w, now), amount, budgetKeyTTL); err != nil {
			return err
		}
	}

	if scopes.RunID != "" {
		if err := c.incrementKey(ctx, runKey(scopes.RunID), amount, runKeyTTL); err != nil {
			return err
		}
	}

	for _, cat := range categories {
		if err := c.incrementKey(ctx, categoryKey(scopes.PrincipalID, cat, now), amount, budgetKeyTTL); err != nil {
			return err
		}
	}
	return nil
}

func (c *RedisBudgetCounter) incrementKey(ctx context.Context, key string, amount float64, ttl time.Duration) error {
	if _, err := c.rdb.IncrByFloat(ctx, key, amount).Result(); err != nil {
		return fmt.Errorf("budget counter incr %q: %w", key, err)
	}

	// Refresh TTL on every write (per ADR).
	if expErr := c.rdb.Expire(ctx, key, ttl).Err(); expErr != nil {
		c.logger.Warn(ctx, "budget counter expire failed",
			logging.Field{Key: "key", Value: key},
			logging.Field{Key: "error", Value: expErr.Error()},
		)
	}
	return nil
}
