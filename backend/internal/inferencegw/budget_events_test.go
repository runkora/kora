package inferencegw

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

func TestCrossedLevels(t *testing.T) {
	cases := []struct {
		before, after float64
		want          []int
	}{
		{4, 4.9, nil},
		{4, 5, []int{50}},
		{5, 6, nil},
		{7, 8, []int{80}},
		{4, 12, []int{50, 80, 100}},
		{10, 11, nil},
	}
	for _, c := range cases {
		if got := crossedLevels(c.before, c.after, 10); !slices.Equal(got, c.want) {
			t.Errorf("crossedLevels(%v, %v, 10) = %v, want %v", c.before, c.after, got, c.want)
		}
	}
}

// errDiscard rolls back a test transaction so no row reaches a running relay.
var errDiscard = errors.New("discard test rows")

func TestPostgres_BudgetEventsRecordOncePerPeriod(t *testing.T) {
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run inference budget event Postgres tests")
	}
	raw, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(raw.Close)
	pool := storage.NewPool(raw)

	tenant := "test-" + uuid.NewString()
	svc := (&Service{
		policy: &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 10},
		logger: logging.NewLoggerWithOutput(io.Discard),
	}).WithEvents(pool)
	p := principal{TenantID: tenant, PrincipalID: "user-" + uuid.NewString()}
	scopes := SpendScopes{TenantID: tenant, PrincipalID: p.PrincipalID, PrincipalWindows: []string{"monthly"}}
	spent := func(v float64) policy.BudgetSpends {
		return policy.BudgetSpends{Principal: map[string]float64{"monthly": v}}
	}
	count := func(ctx context.Context, typ events.Type) int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE tenant_id = $1 AND type = $2`, tenant, string(typ)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	err = storage.WithTransaction(context.Background(), pool, func(ctx context.Context, _ pgx.Tx) error {
		svc.recordThresholds(ctx, p, nil, scopes, spent(6), 2)
		svc.recordThresholds(ctx, p, nil, scopes, spent(6), 2)
		svc.recordThresholds(ctx, p, nil, scopes, spent(10.5), 4.5)
		if n := count(ctx, events.BudgetThresholdCrossed); n != 3 {
			t.Errorf("threshold events = %d, want one each for 50, 80 and 100", n)
		}
		reached := []policy.BudgetCeiling{{Scope: "principal", ID: p.PrincipalID, Window: "monthly", LimitUSD: 10, SpendUSD: 10.5}}
		svc.recordExceeded(ctx, tenant, reached)
		svc.recordExceeded(ctx, tenant, reached)
		if n := count(ctx, events.BudgetExceeded); n != 1 {
			t.Errorf("exceeded events = %d, want 1 per period", n)
		}
		return errDiscard
	})
	if !errors.Is(err, errDiscard) {
		t.Fatal(err)
	}
}
