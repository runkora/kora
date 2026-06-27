package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/redis/go-redis/v9"
)

// ── stubRedisOps ──────────────────────────────────────────────────────────────

// stubRedisOps is an in-memory stand-in for redisOps.
type stubRedisOps struct {
	// mgetVals maps key → string value; missing entry means key absent.
	mgetVals map[string]any
	mgetErr  error

	getVal string
	getErr error

	setErr error

	incrVal float64
	incrErr error

	expireErr error

	// observability
	setKeys   []string
	setValues []string
	incrKeys  []string
}

func (s *stubRedisOps) Get(_ context.Context, _ string) *redis.StringCmd {
	cmd := redis.NewStringCmd(context.Background())
	if s.getErr != nil {
		cmd.SetErr(s.getErr)
	} else {
		cmd.SetVal(s.getVal)
	}
	return cmd
}

func (s *stubRedisOps) MGet(_ context.Context, keys ...string) *redis.SliceCmd {
	cmd := redis.NewSliceCmd(context.Background())
	if s.mgetErr != nil {
		cmd.SetErr(s.mgetErr)
		return cmd
	}
	vals := make([]any, len(keys))
	for i, k := range keys {
		if s.mgetVals != nil {
			vals[i] = s.mgetVals[k]
		}
	}
	cmd.SetVal(vals)
	return cmd
}

func (s *stubRedisOps) Set(_ context.Context, key string, value any, _ time.Duration) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(context.Background())
	s.setKeys = append(s.setKeys, key)
	s.setValues = append(s.setValues, fmt.Sprintf("%v", value))
	if s.setErr != nil {
		cmd.SetErr(s.setErr)
	} else {
		cmd.SetVal("OK")
	}
	return cmd
}

func (s *stubRedisOps) IncrByFloat(_ context.Context, key string, _ float64) *redis.FloatCmd {
	cmd := redis.NewFloatCmd(context.Background())
	if s.incrErr != nil {
		cmd.SetErr(s.incrErr)
		return cmd
	}
	s.incrKeys = append(s.incrKeys, key)
	cmd.SetVal(s.incrVal)
	return cmd
}

func (s *stubRedisOps) Expire(_ context.Context, _ string, _ time.Duration) *redis.BoolCmd {
	cmd := redis.NewBoolCmd(context.Background())
	if s.expireErr != nil {
		cmd.SetErr(s.expireErr)
	} else {
		cmd.SetVal(true)
	}
	return cmd
}

// ── stubUsageRepo ─────────────────────────────────────────────────────────────

type stubUsageRepo struct {
	principalSpend float64
	tenantSpend    float64
	poolSpend      float64
	runSpend       float64
	err            error
}

func (s *stubUsageRepo) GetSpendSince(_ context.Context, _, _ string, _ time.Time) (float64, error) {
	return s.principalSpend, s.err
}

func (s *stubUsageRepo) GetTenantSpendSince(_ context.Context, _ string, _ time.Time) (float64, error) {
	return s.tenantSpend, s.err
}

func (s *stubUsageRepo) GetPoolSpendSince(_ context.Context, _, _ string, _ time.Time) (float64, error) {
	return s.poolSpend, s.err
}

func (s *stubUsageRepo) GetRunSpend(_ context.Context, _, _ string) (float64, error) {
	return s.runSpend, s.err
}

func (s *stubUsageRepo) GetRunUsage(_ context.Context, _, _ string) (float64, int64, error) {
	return s.runSpend, 0, s.err
}

func (s *stubUsageRepo) GetAgentTypeUsage(_ context.Context, _, _ string) (AgentTypeUsage, error) {
	return AgentTypeUsage{}, nil
}

func (s *stubUsageRepo) ListAgentTypeUsage(_ context.Context, _ string) ([]AgentTypeUsage, error) {
	return nil, nil
}

func (s *stubUsageRepo) Create(_ context.Context, _ InferenceUsageRecord) error { return nil }
func (s *stubUsageRepo) List(_ context.Context, _ InferenceUsageFilter) ([]InferenceUsageRecord, bool, error) {
	return nil, false, nil
}
func (s *stubUsageRepo) Get(_ context.Context, _, _ string) (InferenceUsageRecord, error) {
	return InferenceUsageRecord{}, nil
}
func (s *stubUsageRepo) UsageStats(_ context.Context, _, _ string) ([]TokenUsageStat, error) {
	return nil, nil
}
func (s *stubUsageRepo) GetAnalytics(_ context.Context, _ AnalyticsFilter) (AnalyticsResponse, error) {
	return AnalyticsResponse{}, nil
}
func (s *stubUsageRepo) ListPrincipalUsage(_ context.Context, _ AnalyticsPrincipalsFilter) (AnalyticsPrincipalsResponse, error) {
	return AnalyticsPrincipalsResponse{}, nil
}
func (s *stubUsageRepo) GetMonthlySpendByPrincipal(_ context.Context, _ string, _ time.Time) ([]PrincipalPeriodSpend, error) {
	return nil, nil
}
func (s *stubUsageRepo) GetSpendByPrincipalSince(_ context.Context, _ string, _ time.Time) (map[string]float64, error) {
	return nil, nil
}
func (s *stubUsageRepo) GetActiveRoleHolders(_ context.Context, _ string, _ time.Time) (map[string][]string, error) {
	return nil, nil
}
func (s *stubUsageRepo) GetPrincipalRoles(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newBudgetCounter(rdb redisOps, usage InferenceUsageRepository) *RedisBudgetCounter {
	return &RedisBudgetCounter{
		rdb:    rdb,
		usage:  usage,
		logger: logging.NewLoggerWithOutput(io.Discard),
	}
}

func roleCat(name string) policy.BudgetCategory {
	return policy.BudgetCategory{Source: "role", ID: name, ResetCadence: "monthly"}
}

// ── periods and keys ──────────────────────────────────────────────────────────

func TestPeriodForCadence_Formats(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC) // Wednesday, ISO week 24
	cases := map[string]string{
		"monthly":      "2026-06",
		"weekly":       "2026-W24",
		"unrecognized": "2026-06",
	}
	for cadence, want := range cases {
		if got := periodForCadence(cadence, ts); got != want {
			t.Errorf("periodForCadence(%q) = %q, want %q", cadence, got, want)
		}
	}
}

func TestPeriodStart_WeeklyIsISOMonday(t *testing.T) {
	t.Parallel()
	wednesday := time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC)
	want := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC) // Monday of that week
	if got := periodStart("weekly", wednesday); !got.Equal(want) {
		t.Errorf("periodStart(weekly) = %v, want %v", got, want)
	}
	// Sunday belongs to the week that started the previous Monday.
	sunday := time.Date(2026, 6, 14, 1, 0, 0, 0, time.UTC)
	if got := periodStart("weekly", sunday); !got.Equal(want) {
		t.Errorf("periodStart(weekly, sunday) = %v, want %v", got, want)
	}
}

func TestKeys_Formats(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	if got, want := principalKey("pid-1", "monthly", ts), "budget:pid-1:2026-06"; got != want {
		t.Errorf("principalKey = %q, want %q", got, want)
	}
	if got, want := principalKey("pid-1", "weekly", ts), "budget:pid-1:2026-W24"; got != want {
		t.Errorf("principalKey weekly = %q, want %q", got, want)
	}
	if got, want := tenantKey("t1", "monthly", ts), "budget:tenant:t1:2026-06"; got != want {
		t.Errorf("tenantKey = %q, want %q", got, want)
	}
	if got, want := poolKey("t1", "engineering", "weekly", ts), "budget:rolepool:t1:engineering:2026-W24"; got != want {
		t.Errorf("poolKey = %q, want %q", got, want)
	}
	if got, want := runKey("run-42"), "budget:run:run-42"; got != want {
		t.Errorf("runKey = %q, want %q", got, want)
	}
	cat := policy.BudgetCategory{Source: "role", ID: "engineer", ResetCadence: "monthly"}
	if got, want := categoryKey("pid-1", cat, ts), "budget:pid-1:role:engineer:2026-06"; got != want {
		t.Errorf("categoryKey = %q, want %q", got, want)
	}
}

// ── GetSpends ─────────────────────────────────────────────────────────────────

func TestGetSpends_ReadsAllScopes(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	rdb := &stubRedisOps{mgetVals: map[string]any{
		principalKey("pid-1", "monthly", now):       "12.5",
		principalKey("pid-1", "weekly", now):        "3.25",
		tenantKey("t1", "monthly", now):             "1800",
		poolKey("t1", "engineering", "weekly", now): "240",
		runKey("run-42"):                            "1.5",
	}}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	spends, err := c.GetSpends(context.Background(), SpendScopes{
		TenantID:         "t1",
		PrincipalID:      "pid-1",
		PrincipalWindows: []string{"monthly", "weekly"},
		TenantWindow:     "monthly",
		PoolWindows:      map[string]string{"engineering": "weekly"},
		RunID:            "run-42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spends.Principal["monthly"] != 12.5 || spends.Principal["weekly"] != 3.25 {
		t.Errorf("principal spends = %v", spends.Principal)
	}
	if spends.Tenant["monthly"] != 1800 {
		t.Errorf("tenant spends = %v", spends.Tenant)
	}
	if spends.RolePools["engineering"] != 240 {
		t.Errorf("pool spends = %v", spends.RolePools)
	}
	if spends.Run != 1.5 {
		t.Errorf("run spend = %v, want 1.5", spends.Run)
	}
}

func TestGetSpends_MissesRehydrateFromDB(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{} // every key missing
	usage := &stubUsageRepo{principalSpend: 40, tenantSpend: 900, poolSpend: 120, runSpend: 2.5}
	c := newBudgetCounter(rdb, usage)

	spends, err := c.GetSpends(context.Background(), SpendScopes{
		TenantID:         "t1",
		PrincipalID:      "pid-1",
		PrincipalWindows: []string{"monthly"},
		TenantWindow:     "weekly",
		PoolWindows:      map[string]string{"engineering": "monthly"},
		RunID:            "run-42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spends.Principal["monthly"] != 40 || spends.Tenant["weekly"] != 900 ||
		spends.RolePools["engineering"] != 120 || spends.Run != 2.5 {
		t.Errorf("rehydrated spends = %+v", spends)
	}
	// Every miss primes its key.
	if len(rdb.setKeys) != 4 {
		t.Errorf("primed %d keys, want 4: %v", len(rdb.setKeys), rdb.setKeys)
	}
}

func TestGetSpends_NoScopes_NoReads(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{mgetErr: errors.New("must not be called")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	spends, err := c.GetSpends(context.Background(), SpendScopes{TenantID: "t1", PrincipalID: "pid-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spends.Run != 0 || len(spends.Principal) != 0 {
		t.Errorf("expected empty spends, got %+v", spends)
	}
}

func TestGetSpends_MGetError(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{mgetErr: errors.New("redis unavailable")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	_, err := c.GetSpends(context.Background(), SpendScopes{
		TenantID: "t1", PrincipalID: "pid-1", PrincipalWindows: []string{"monthly"},
	})
	if err == nil {
		t.Fatal("expected error on MGET failure, got nil")
	}
}

func TestGetSpends_RehydrateDBError_Propagates(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{}
	usage := &stubUsageRepo{err: errors.New("db down")}
	c := newBudgetCounter(rdb, usage)

	_, err := c.GetSpends(context.Background(), SpendScopes{
		TenantID: "t1", PrincipalID: "pid-1", PrincipalWindows: []string{"monthly"},
	})
	if err == nil {
		t.Fatal("expected rehydration error, got nil")
	}
}

// ── GetCategorySpends ────────────────────────────────────────────────────────

func TestGetCategorySpends_AlignedToInput(t *testing.T) {
	t.Parallel()
	cats := []policy.BudgetCategory{
		roleCat("eng"),
		{Source: "grant", ID: "g1", ResetCadence: "monthly"},
	}
	now := time.Now().UTC()
	rdb := &stubRedisOps{
		mgetVals: map[string]any{categoryKey("pid-1", cats[0], now): "30.5"}, // grant key absent → 0
	}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	got, err := c.GetCategorySpends(context.Background(), "pid-1", cats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != 30.5 || got[1] != 0 {
		t.Errorf("category spends = %v, want [30.5 0]", got)
	}
}

// ── IncrementSpends ──────────────────────────────────────────────────────────

func TestIncrementSpends_WritesEveryApplicableCounter(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{incrVal: 1}
	c := newBudgetCounter(rdb, &stubUsageRepo{})
	now := time.Now().UTC()
	cats := []policy.BudgetCategory{roleCat("eng")}

	err := c.IncrementSpends(context.Background(), SpendScopes{
		TenantID:         "t1",
		PrincipalID:      "pid-1",
		PrincipalWindows: []string{"weekly"},
		TenantWindow:     "monthly",
		PoolWindows:      map[string]string{"engineering": "weekly"},
		RunID:            "run-42",
	}, cats, 0.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		principalKey("pid-1", "monthly", now), // always written
		principalKey("pid-1", "weekly", now),
		tenantKey("t1", "monthly", now),
		poolKey("t1", "engineering", "weekly", now),
		runKey("run-42"),
		categoryKey("pid-1", cats[0], now),
	}
	for _, k := range want {
		if !slices.Contains(rdb.incrKeys, k) {
			t.Errorf("expected increment of %q, got %v", k, rdb.incrKeys)
		}
	}
	if len(rdb.incrKeys) != len(want) {
		t.Errorf("incremented %d keys, want %d: %v", len(rdb.incrKeys), len(want), rdb.incrKeys)
	}
}

func TestIncrementSpends_MinimalScopes_CanonicalMonthlyOnly(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{incrVal: 1}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	if err := c.IncrementSpends(context.Background(), SpendScopes{TenantID: "t1", PrincipalID: "pid-1"}, nil, 0.5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantKey := principalKey("pid-1", "monthly", time.Now().UTC())
	if len(rdb.incrKeys) != 1 || rdb.incrKeys[0] != wantKey {
		t.Errorf("incremented keys = %v, want [%q]", rdb.incrKeys, wantKey)
	}
}

func TestIncrementSpends_IncrError_Propagates(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{incrErr: errors.New("redis unavailable")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	err := c.IncrementSpends(context.Background(), SpendScopes{TenantID: "t1", PrincipalID: "pid-1"}, nil, 0.5)
	if err == nil {
		t.Fatal("expected error from INCRBYFLOAT failure, got nil")
	}
}

func TestIncrementSpends_ExpireError_NonFatal(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{incrVal: 1, expireErr: errors.New("expire failed")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	if err := c.IncrementSpends(context.Background(), SpendScopes{TenantID: "t1", PrincipalID: "pid-1"}, nil, 0.5); err != nil {
		t.Errorf("Expire error should not propagate, got: %v", err)
	}
}
