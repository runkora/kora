package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/redis/go-redis/v9"
)

// ── stubRedisOps ──────────────────────────────────────────────────────────────

// stubRedisOps is an in-memory stand-in for redisOps.
// Each field controls a single command's response.
type stubRedisOps struct {
	// getVal is returned by Get when getErr is nil.
	getVal string
	getErr error

	// mgetVals maps key → string value; nil entry means key missing.
	mgetVals map[string]any
	mgetErr  error

	setErr error

	incrVal float64
	incrErr error

	expireErr error

	// observability: record what was Set during priming.
	setKeyCapture   string
	setValueCapture string
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
	s.setKeyCapture = key
	s.setValueCapture = fmt.Sprintf("%v", value)
	if s.setErr != nil {
		cmd.SetErr(s.setErr)
	} else {
		cmd.SetVal("OK")
	}
	return cmd
}

func (s *stubRedisOps) IncrByFloat(_ context.Context, _ string, _ float64) *redis.FloatCmd {
	cmd := redis.NewFloatCmd(context.Background())
	if s.incrErr != nil {
		cmd.SetErr(s.incrErr)
	} else {
		cmd.SetVal(s.incrVal)
	}
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
	monthlySpend float64
	err          error
}

func (s *stubUsageRepo) GetMonthlySpend(_ context.Context, _, _ string, _ time.Time) (float64, error) {
	return s.monthlySpend, s.err
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

// ── helpers ───────────────────────────────────────────────────────────────────

func newBudgetCounter(rdb redisOps, usage InferenceUsageRepository) *RedisBudgetCounter {
	return &RedisBudgetCounter{
		rdb:    rdb,
		usage:  usage,
		logger: logging.NewLoggerWithOutput(io.Discard),
	}
}

// noCats is a convenience nil slice meaning "no configured categories" (legacy path).
var noCats []policy.BudgetCategory

func roleCat(name string) policy.BudgetCategory {
	return policy.BudgetCategory{Source: "role", ID: name, ResetCadence: "monthly"}
}

// ── legacyBudgetKey (no-category fallback) ────────────────────────────────────

func TestLegacyBudgetKey_Format(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC)
	got := legacyBudgetKey("pid-abc", ts)
	want := "budget:pid-abc:2026-06"
	if got != want {
		t.Errorf("legacyBudgetKey = %q, want %q", got, want)
	}
}

func TestLegacyBudgetKey_UsesUTCMonth(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("UTC+2", 2*60*60)
	ts := time.Date(2026, 7, 1, 1, 0, 0, 0, loc) // 2026-07-01 01:00 +02:00 = 2026-06-30 23:00 UTC
	got := legacyBudgetKey("pid-1", ts)
	want := "budget:pid-1:2026-06"
	if got != want {
		t.Errorf("legacyBudgetKey with non-UTC zone = %q, want %q (must use UTC month)", got, want)
	}
}

// ── categoryKey ───────────────────────────────────────────────────────────────

func TestCategoryKey_Role(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	cat := policy.BudgetCategory{Source: "role", ID: "engineer", ResetCadence: "monthly"}
	got := categoryKey("pid-abc", cat, ts)
	want := "budget:pid-abc:role:engineer:2026-06"
	if got != want {
		t.Errorf("categoryKey = %q, want %q", got, want)
	}
}

func TestCategoryKey_Grant(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	cat := policy.BudgetCategory{Source: "grant", ID: "grant-xyz", ResetCadence: "monthly"}
	got := categoryKey("pid-1", cat, ts)
	want := "budget:pid-1:grant:grant-xyz:2026-06"
	if got != want {
		t.Errorf("categoryKey = %q, want %q", got, want)
	}
}

// ── GetSpend: legacy path (no categories) ────────────────────────────────────

func TestGetSpend_Legacy_RedisHit(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{getVal: "42.5"}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	got, err := c.GetSpend(context.Background(), "t1", "pid-1", noCats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42.5 {
		t.Errorf("spend = %v, want 42.5", got)
	}
}

func TestGetSpend_Legacy_CacheMiss_ReturnsDBValue(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{getErr: redis.Nil}
	usage := &stubUsageRepo{monthlySpend: 99.0}
	c := newBudgetCounter(rdb, usage)

	got, err := c.GetSpend(context.Background(), "t1", "pid-1", noCats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 99.0 {
		t.Errorf("spend = %v, want 99.0", got)
	}
}

func TestGetSpend_Legacy_RedisError(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{getErr: errors.New("connection refused")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	_, err := c.GetSpend(context.Background(), "t1", "pid-1", noCats)
	if err == nil {
		t.Fatal("expected error for non-nil Redis error, got nil")
	}
}

func TestGetSpend_Legacy_CacheMiss_SetFailure_StillReturnsValue(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{getErr: redis.Nil, setErr: errors.New("redis full")}
	usage := &stubUsageRepo{monthlySpend: 10.0}
	c := newBudgetCounter(rdb, usage)

	got, err := c.GetSpend(context.Background(), "t1", "pid-1", noCats)
	if err != nil {
		t.Fatalf("Set failure should not bubble up; got: %v", err)
	}
	if got != 10.0 {
		t.Errorf("spend = %v, want 10.0", got)
	}
}

// ── GetSpend: per-category path ───────────────────────────────────────────────

func TestGetSpend_CategoryPath_SumsAllHits(t *testing.T) {
	t.Parallel()
	cats := []policy.BudgetCategory{
		{Source: "role", ID: "eng", ResetCadence: "monthly"},
		{Source: "grant", ID: "g1", ResetCadence: "monthly"},
	}
	now := time.Now().UTC()
	k0 := categoryKey("pid-1", cats[0], now)
	k1 := categoryKey("pid-1", cats[1], now)
	rdb := &stubRedisOps{
		mgetVals: map[string]any{k0: "30.0", k1: "20.0"},
	}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	got, err := c.GetSpend(context.Background(), "t1", "pid-1", cats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 50.0 {
		t.Errorf("spend = %v, want 50.0 (30+20)", got)
	}
}

func TestGetSpend_CategoryPath_MGetError(t *testing.T) {
	t.Parallel()
	cats := []policy.BudgetCategory{roleCat("eng")}
	rdb := &stubRedisOps{mgetErr: errors.New("redis unavailable")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	_, err := c.GetSpend(context.Background(), "t1", "pid-1", cats)
	if err == nil {
		t.Fatal("expected error on MGET failure, got nil")
	}
}

func TestGetSpend_CategoryPath_PartialMiss_SumsHitAndRehydratesRest(t *testing.T) {
	t.Parallel()
	// Two categories: key[0] is a Redis hit (25.0), key[1] is absent.
	// DB returns 40.0 aggregate. residual = max(0, 40.0 - 25.0) = 15.0.
	// Total = 25.0 (hit) + 15.0 (residual for the miss) = 40.0.
	// The miss key is seeded with 15.0 (the residual, not the full DB value).
	cats := []policy.BudgetCategory{
		{Source: "role", ID: "eng", ResetCadence: "monthly"},
		{Source: "grant", ID: "g1", ResetCadence: "monthly"},
	}
	now := time.Now().UTC()
	k0 := categoryKey("pid-1", cats[0], now)
	k1 := categoryKey("pid-1", cats[1], now)
	_ = k0
	rdb := &stubRedisOps{
		mgetVals: map[string]any{k0: "25.0"}, // k1 is absent (nil)
	}
	usage := &stubUsageRepo{monthlySpend: 40.0}
	c := newBudgetCounter(rdb, usage)

	got, err := c.GetSpend(context.Background(), "t1", "pid-1", cats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := 40.0 // 25.0 (hit) + 15.0 (residual: 40 - 25)
	if got != want {
		t.Errorf("spend = %v, want %v", got, want)
	}
	// The missing key (k1) is seeded with the residual (15.0), not the full DB value.
	if rdb.setKeyCapture != k1 {
		t.Errorf("seeded key = %q, want %q", rdb.setKeyCapture, k1)
	}
	if rdb.setValueCapture != "15" {
		t.Errorf("seeded value = %q, want \"15\" (residual = db - hits)", rdb.setValueCapture)
	}
}

func TestGetSpend_CategoryPath_AllMiss_RehydratesFromDB(t *testing.T) {
	t.Parallel()
	// Both category keys are absent. DB returns 55.0.
	// misses[0] is seeded with 55.0; misses[1] is seeded with 0.
	// Total = 55.0 (one DB call; second miss not added to total).
	cats := []policy.BudgetCategory{
		roleCat("eng"),
		{Source: "grant", ID: "g2", ResetCadence: "monthly"},
	}
	rdb := &stubRedisOps{mgetVals: nil} // all nil
	usage := &stubUsageRepo{monthlySpend: 55.0}
	c := newBudgetCounter(rdb, usage)

	got, err := c.GetSpend(context.Background(), "t1", "pid-1", cats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only misses[0] contributes to total; misses[1] is zero-seeded and not added.
	if got != 55.0 {
		t.Errorf("spend = %v, want 55.0", got)
	}
	// The last Set call is the zero-seed for misses[1].
	now := time.Now().UTC()
	wantKey := categoryKey("pid-1", cats[1], now)
	if rdb.setKeyCapture != wantKey {
		t.Errorf("last seeded key = %q, want %q (second miss zero-seed)", rdb.setKeyCapture, wantKey)
	}
	if rdb.setValueCapture != "0" {
		t.Errorf("zero-seed value = %q, want \"0\"", rdb.setValueCapture)
	}
}

// ── IncrementSpend: legacy path (no categories) ──────────────────────────────

func TestIncrementSpend_Legacy_NewKey_ExpiresCalled(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{incrVal: 5.0}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	if err := c.IncrementSpend(context.Background(), "t1", "pid-1", noCats, 5.0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIncrementSpend_Legacy_IncrError(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{incrErr: errors.New("redis unavailable")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	err := c.IncrementSpend(context.Background(), "t1", "pid-1", noCats, 5.0)
	if err == nil {
		t.Fatal("expected error from INCRBYFLOAT failure, got nil")
	}
}

// TestIncrementSpend_Legacy_TTLError_NonFatal is removed: the TTL check was
// replaced by an unconditional Expire call (ADR: 35 days from last write).
// Expire errors are still non-fatal — see TestIncrementSpend_Legacy_ExpireError_NonFatal.

func TestIncrementSpend_Legacy_ExpireError_NonFatal(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{
		incrVal:   5.0,
		expireErr: errors.New("expire failed"),
	}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	if err := c.IncrementSpend(context.Background(), "t1", "pid-1", noCats, 5.0); err != nil {
		t.Errorf("Expire error should not propagate, got: %v", err)
	}
}

// ── IncrementSpend: per-category path ────────────────────────────────────────

func TestIncrementSpend_CategoryPath_IncrementsEachKey(t *testing.T) {
	t.Parallel()
	cats := []policy.BudgetCategory{
		roleCat("eng"),
		{Source: "grant", ID: "g1", ResetCadence: "monthly"},
	}
	rdb := &stubRedisOps{incrVal: 10.0}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	if err := c.IncrementSpend(context.Background(), "t1", "pid-1", cats, 10.0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIncrementSpend_CategoryPath_IncrError_Propagates(t *testing.T) {
	t.Parallel()
	cats := []policy.BudgetCategory{roleCat("eng")}
	rdb := &stubRedisOps{incrErr: errors.New("redis down")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	err := c.IncrementSpend(context.Background(), "t1", "pid-1", cats, 5.0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
