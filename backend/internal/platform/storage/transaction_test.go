package storage

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeTx records commit and rollback. Calls the tests do not make panic on
// the nil embedded interface.
type fakeTx struct {
	pgx.Tx
	committed, rolledBack bool
}

func (f *fakeTx) Commit(context.Context) error   { f.committed = true; return nil }
func (f *fakeTx) Rollback(context.Context) error { f.rolledBack = true; return nil }

type fakeBeginner struct {
	tx    *fakeTx
	begun int
}

func (f *fakeBeginner) Begin(context.Context) (pgx.Tx, error) {
	f.begun++
	return f.tx, nil
}

func TestWithTransaction_NestedCallJoins(t *testing.T) {
	db := &fakeBeginner{tx: &fakeTx{}}
	err := WithTransaction(context.Background(), db, func(ctx context.Context, outer pgx.Tx) error {
		if got, ok := TxFromContext(ctx); !ok || got != outer {
			t.Fatal("ctx does not carry the open transaction")
		}
		return WithTransaction(ctx, db, func(_ context.Context, inner pgx.Tx) error {
			if inner != outer {
				t.Error("nested call opened a second transaction")
			}
			if db.tx.committed {
				t.Error("nested call committed")
			}
			return nil
		})
	})
	if err != nil || db.begun != 1 || !db.tx.committed {
		t.Fatalf("err=%v begun=%d committed=%v, want one committed transaction", err, db.begun, db.tx.committed)
	}
}

func TestWithTransaction_ErrorRollsBack(t *testing.T) {
	db := &fakeBeginner{tx: &fakeTx{}}
	want := errors.New("boom")
	err := WithTransaction(context.Background(), db, func(ctx context.Context, _ pgx.Tx) error {
		return WithTransaction(ctx, db, func(context.Context, pgx.Tx) error { return want })
	})
	if !errors.Is(err, want) || db.tx.committed || !db.tx.rolledBack {
		t.Fatalf("err=%v committed=%v rolledBack=%v, want the error and a rollback", err, db.tx.committed, db.tx.rolledBack)
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run storage Postgres tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func xactID(t *testing.T, ctx context.Context, p *Pool) string {
	t.Helper()
	var id string
	if err := p.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&id); err != nil {
		t.Fatalf("read transaction id: %v", err)
	}
	return id
}

func TestPostgres_PoolJoinsNestedTransaction(t *testing.T) {
	p := NewPool(testPool(t))
	ctx := context.Background()
	err := WithTransaction(ctx, p, func(ctx context.Context, _ pgx.Tx) error {
		outer := xactID(t, ctx, p)
		return WithTransaction(ctx, p, func(ctx context.Context, _ pgx.Tx) error {
			if inner := xactID(t, ctx, p); inner != outer {
				t.Errorf("nested call ran in transaction %s, want %s", inner, outer)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if a, b := xactID(t, ctx, p), xactID(t, ctx, p); a == b {
		t.Error("calls outside a transaction shared one")
	}
}

func TestPostgres_PoolBeginInTransactionIsSavepoint(t *testing.T) {
	p := NewPool(testPool(t))
	ctx := context.Background()
	err := WithTransaction(ctx, p, func(ctx context.Context, _ pgx.Tx) error {
		if _, err := p.Exec(ctx, `CREATE TEMP TABLE savepoint_probe (v INT) ON COMMIT DROP`); err != nil {
			return err
		}
		sp, err := p.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := sp.Exec(ctx, `INSERT INTO savepoint_probe VALUES (1)`); err != nil {
			return err
		}
		if err := sp.Rollback(ctx); err != nil {
			return err
		}
		var n int
		if err := p.QueryRow(ctx, `SELECT count(*) FROM savepoint_probe`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("savepoint rollback left %d rows", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
