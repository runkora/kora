package declarative

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// errDiscard rolls back a test transaction so no row reaches a running relay.
var errDiscard = errors.New("discard test rows")

func TestPostgres_DeclaredFailureRecordsOncePerGeneration(t *testing.T) {
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run declared failure Postgres tests")
	}
	raw, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(raw.Close)
	pool := storage.NewPool(raw)
	c := NewController(nil, logging.NewLoggerWithOutput(io.Discard), nil).WithEvents(pool)
	tenant := "test-" + uuid.NewString()
	item := Item{TenantID: tenant, Namespace: "kora-tenant-x", Name: "review", UID: uuid.NewString(), Generation: 1}
	conflict := Result{Outcome: OutcomeNameConflict, Message: "an item created through the API already has this name"}

	err = storage.WithTransaction(context.Background(), pool, func(ctx context.Context, _ pgx.Tx) error {
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/internal/declared/triggers", nil)
		r.SetPathValue("kind", KindTriggers)
		c.recordFailure(r, item, conflict)
		c.recordFailure(r, item, conflict)
		c.recordFailure(r, item, Result{Outcome: OutcomeProjected})
		c.recordFailure(r, Item{TenantID: PlatformTenant, UID: item.UID, Name: "vm"}, conflict)
		next := item
		next.Generation = 2
		c.recordFailure(r, next, conflict)

		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE tenant_id = $1`, tenant).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Errorf("failure events = %d, want one per generation", n)
		}
		var leaked bool
		if err := pool.QueryRow(ctx, `SELECT bool_or(envelope::text LIKE '%already has this name%') FROM event_outbox WHERE tenant_id = $1`, tenant).Scan(&leaked); err != nil {
			t.Fatal(err)
		}
		if leaked {
			t.Error("a failure event carries the free-text message")
		}
		var platform int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE tenant_id = $1`, PlatformTenant).Scan(&platform); err != nil {
			t.Fatal(err)
		}
		if platform != 0 {
			t.Errorf("a platform-wide kind recorded %d events", platform)
		}
		return errDiscard
	})
	if !errors.Is(err, errDiscard) {
		t.Fatal(err)
	}
}
