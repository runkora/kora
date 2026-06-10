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

	// scanKeys is returned (in a single page) by Scan.
	scanKeys []string
	scanErr  error

	// observability: record what was Set during priming and which keys
	// were incremented, in order.
	setKeyCapture   string
	setValueCapture string
	incrKeys        []string
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

func (s *stubRedisOps) Scan(_ context.Context, _ uint64, _ string, _ int64) *redis.ScanCmd {
	cmd := redis.NewScanCmd(context.Background(), nil)
	if s.scanErr != nil {
		cmd.SetErr(s.scanErr)
		return cmd
	}
	cmd.SetVal(s.scanKeys, 0)
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
func (s *stubUsageRepo) GetMonthlySpendByPrincipal(_ context.Context, _ string, _ time.Time) ([]PrincipalPeriodSpend, error) {
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

// noCats is a convenience nil slice meaning "no configured budget categories".
var noCats []policy.BudgetCategory

func roleCat(name string) policy.BudgetCategory {
	return policy.BudgetCategory{Source: "role", ID: name, ResetCadence: "monthly"}
}

// ── canonicalBudgetKey ────────────────────────────────────────────────────────

func TestCanonicalBudgetKey_Format(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC)
	got := canonicalBudgetKey("pid-abc", ts)
	want := "budget:pid-abc:2026-06"
	if got != want {
		t.Errorf("canonicalBudgetKey = %q, want %q", got, want)
	}
}

func TestCanonicalBudgetKey_UsesUTCMonth(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("UTC+2", 2*60*60)
	ts := time.Date(2026, 7, 1, 1, 0, 0, 0, loc) // 2026-07-01 01:00 +02:00 = 2026-06-30 23:00 UTC
	got := canonicalBudgetKey("pid-1", ts)
	want := "budget:pid-1:2026-06"
	if got != want {
		t.Errorf("canonicalBudgetKey with non-UTC zone = %q, want %q (must use UTC month)", got, want)
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

// ── GetSpend (canonical counter) ─────────────────────────────────────────────

func TestGetSpend_RedisHit(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{getVal: "42.5"}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	got, err := c.GetSpend(context.Background(), "t1", "pid-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42.5 {
		t.Errorf("spend = %v, want 42.5", got)
	}
}

func TestGetSpend_CacheMiss_RehydratesFromDB(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{getErr: redis.Nil}
	usage := &stubUsageRepo{monthlySpend: 99.0}
	c := newBudgetCounter(rdb, usage)

	got, err := c.GetSpend(context.Background(), "t1", "pid-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 99.0 {
		t.Errorf("spend = %v, want 99.0", got)
	}
	// The canonical key is primed with the DB value.
	wantKey := canonicalBudgetKey("pid-1", time.Now().UTC())
	if rdb.setKeyCapture != wantKey {
		t.Errorf("primed key = %q, want %q", rdb.setKeyCapture, wantKey)
	}
	if rdb.setValueCapture != "99" {
		t.Errorf("primed value = %q, want \"99\"", rdb.setValueCapture)
	}
}

func TestGetSpend_RedisError(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{getErr: errors.New("connection refused")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	_, err := c.GetSpend(context.Background(), "t1", "pid-1")
	if err == nil {
		t.Fatal("expected error for non-nil Redis error, got nil")
	}
}

func TestGetSpend_CacheMiss_SetFailure_StillReturnsValue(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{getErr: redis.Nil, setErr: errors.New("redis full")}
	usage := &stubUsageRepo{monthlySpend: 10.0}
	c := newBudgetCounter(rdb, usage)

	got, err := c.GetSpend(context.Background(), "t1", "pid-1")
	if err != nil {
		t.Fatalf("Set failure should not bubble up; got: %v", err)
	}
	if got != 10.0 {
		t.Errorf("spend = %v, want 10.0", got)
	}
}

func TestGetSpend_CacheMiss_DBError_Propagates(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{getErr: redis.Nil}
	usage := &stubUsageRepo{err: errors.New("db down")}
	c := newBudgetCounter(rdb, usage)

	_, err := c.GetSpend(context.Background(), "t1", "pid-1")
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
	k0 := categoryKey("pid-1", cats[0], now)
	rdb := &stubRedisOps{
		mgetVals: map[string]any{k0: "30.5"}, // grant key absent → 0
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

func TestGetCategorySpends_NoCategories_NilResult(t *testing.T) {
	t.Parallel()
	c := newBudgetCounter(&stubRedisOps{}, &stubUsageRepo{})

	got, err := c.GetCategorySpends(context.Background(), "pid-1", noCats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for no categories, got %v", got)
	}
}

func TestGetCategorySpends_MGetError(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{mgetErr: errors.New("redis unavailable")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	_, err := c.GetCategorySpends(context.Background(), "pid-1", []policy.BudgetCategory{roleCat("eng")})
	if err == nil {
		t.Fatal("expected error on MGET failure, got nil")
	}
}

// ── IncrementSpend ────────────────────────────────────────────────────────────

func TestIncrementSpend_NoCategories_WritesCanonicalOnly(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{incrVal: 5.0}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	if err := c.IncrementSpend(context.Background(), "t1", "pid-1", noCats, 5.0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantKey := canonicalBudgetKey("pid-1", time.Now().UTC())
	if len(rdb.incrKeys) != 1 || rdb.incrKeys[0] != wantKey {
		t.Errorf("incremented keys = %v, want [%q]", rdb.incrKeys, wantKey)
	}
}

func TestIncrementSpend_WithCategories_WritesCanonicalAndEachCategory(t *testing.T) {
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
	now := time.Now().UTC()
	want := []string{
		canonicalBudgetKey("pid-1", now),
		categoryKey("pid-1", cats[0], now),
		categoryKey("pid-1", cats[1], now),
	}
	if len(rdb.incrKeys) != len(want) {
		t.Fatalf("incremented %d keys, want %d: %v", len(rdb.incrKeys), len(want), rdb.incrKeys)
	}
	for i, k := range want {
		if rdb.incrKeys[i] != k {
			t.Errorf("incrKeys[%d] = %q, want %q", i, rdb.incrKeys[i], k)
		}
	}
}

func TestIncrementSpend_IncrError_Propagates(t *testing.T) {
	t.Parallel()
	rdb := &stubRedisOps{incrErr: errors.New("redis unavailable")}
	c := newBudgetCounter(rdb, &stubUsageRepo{})

	err := c.IncrementSpend(context.Background(), "t1", "pid-1", noCats, 5.0)
	if err == nil {
		t.Fatal("expected error from INCRBYFLOAT failure, got nil")
	}
}

func TestIncrementSpend_ExpireError_NonFatal(t *testing.T) {
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
