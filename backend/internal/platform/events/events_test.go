package events

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func runFailed(tenant string) Event {
	return Event{Type: RunFailed, Tenant: tenant, Subject: "runs/1", RunID: uuid.NewString(), Entity: map[string]string{"state": "failed"}}
}

func withPrincipal(sub string, typ token.PrincipalType) context.Context {
	claims := &token.PlatformClaims{}
	claims.Subject = sub
	claims.Kora.PrincipalType = typ
	return token.WithClaims(context.Background(), claims)
}

func TestNewEnvelope_Principal(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ctx           context.Context
		wantPrincipal string
		wantType      string
	}{
		{"no claims", context.Background(), source, PrincipalSystem},
		{"person", withPrincipal("alice", token.PrincipalTypeHuman), "alice", PrincipalUser},
		{"pat", withPrincipal("alice", token.PrincipalTypePAT), "alice", PrincipalUser},
		{"agent", withPrincipal("spiffe://td/agent", token.PrincipalTypeAgent), "spiffe://td/agent", PrincipalAgent},
		{"trigger", withPrincipal("trigger:1", token.PrincipalTypeService), "trigger:1", PrincipalTrigger},
		{"webhook source", withPrincipal("trigger-source:gh", token.PrincipalTypeService), "trigger-source:gh", PrincipalTrigger},
		{"tenant service", withPrincipal("spiffe://td/tenant/t/service/s", token.PrincipalTypeService), "spiffe://td/tenant/t/service/s", PrincipalSystem},
		{"platform", withPrincipal("kora-agents", token.PrincipalTypeSystem), "kora-agents", PrincipalSystem},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, err := newEnvelope(tc.ctx, uuid.New(), runFailed("t1"), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if env.Principal != tc.wantPrincipal || env.PrincipalType != tc.wantType {
				t.Errorf("principal = %q %q, want %q %q", env.Principal, env.PrincipalType, tc.wantPrincipal, tc.wantType)
			}
		})
	}
}

func TestNewEnvelope_Attributes(t *testing.T) {
	ctx := policy.WithDelegation(withPrincipal("agent", token.PrincipalTypeAgent), &policy.DelegationClaims{Sub: "alice", ActSub: "agent"})
	e := runFailed("t1")
	e.Agent, e.ChainDepth, e.Triggers, e.Changed, e.Version = "triage", 2, []string{"trg"}, []string{"state"}, 7
	id := uuid.New()
	env, err := newEnvelope(ctx, id, e, time.Date(2026, 11, 6, 3, 14, 7, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := Envelope{
		SpecVersion: "1.0", ID: id.String(), Source: source, Type: RunFailed, Subject: "runs/1",
		Time: "2026-11-06T03:14:07Z", DataContentType: "application/json", Tenant: "t1",
		Principal: "agent", PrincipalType: PrincipalAgent, Delegator: "alice", Run: e.RunID, Agent: "triage",
		ChainDepth: 2, Triggers: []string{"trg"},
		Data: Data{Entity: e.Entity, Changed: []string{"state"}, EntityVersion: 7, OccurredAt: "2026-11-06T03:14:07Z"},
	}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("envelope = %+v, want %+v", env, want)
	}
}

func TestNewEnvelope_Refuses(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(*Event)
		want error
	}{
		"no tenant":      {func(e *Event) { e.Tenant = "" }, ErrNoTenant},
		"unknown type":   {func(e *Event) { e.Type = "agents.run.vanished.v1" }, ErrInvalidEvent},
		"no subject":     {func(e *Event) { e.Subject = "" }, ErrInvalidEvent},
		"run not a uuid": {func(e *Event) { e.RunID = "run-1" }, ErrInvalidEvent},
		"cause not uuid": {func(e *Event) { e.CausedBy = "x" }, ErrInvalidEvent},
		"negative depth": {func(e *Event) { e.ChainDepth = -1 }, ErrInvalidEvent},
	} {
		t.Run(name, func(t *testing.T) {
			e := runFailed("t1")
			tc.edit(&e)
			if _, err := newEnvelope(context.Background(), uuid.New(), e, time.Now()); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCatalog(t *testing.T) {
	for typ := range catalog {
		parts := strings.Split(string(typ), ".")
		if len(parts) != 4 || parts[3] != "v1" {
			t.Errorf("%s is not <domain>.<entity>.<event>.v1", typ)
		}
	}
	if got := RunFailed.Category(); got != "agents.run" {
		t.Errorf("Category = %q, want agents.run", got)
	}
}

func testPool(t *testing.T) *storage.Pool {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run events Postgres tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return storage.NewPool(pool)
}

// errDiscard rolls back a test transaction so no row reaches a running relay.
var errDiscard = errors.New("discard test rows")

// inDiscardedTx runs fn in a transaction that is always rolled back.
func inDiscardedTx(t *testing.T, pool *storage.Pool, fn func(ctx context.Context) error) {
	t.Helper()
	err := storage.WithTransaction(context.Background(), pool, func(ctx context.Context, _ pgx.Tx) error {
		if err := fn(ctx); err != nil {
			return err
		}
		return errDiscard
	})
	if !errors.Is(err, errDiscard) {
		t.Fatal(err)
	}
}

func outboxRows(t *testing.T, ctx context.Context, pool *storage.Pool, tenant string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE tenant_id = $1`, tenant).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPostgres_PublishJoinsTheTransaction(t *testing.T) {
	pool := testPool(t)
	tenant := "test-" + uuid.NewString()

	failed := errors.New("state change failed")
	err := storage.WithTransaction(context.Background(), pool, func(ctx context.Context, _ pgx.Tx) error {
		if err := Publish(ctx, pool, runFailed(tenant)); err != nil {
			return err
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("err = %v", err)
	}
	if n := outboxRows(t, context.Background(), pool, tenant); n != 0 {
		t.Fatalf("rolled-back transaction left %d outbox rows", n)
	}

	inDiscardedTx(t, pool, func(ctx context.Context) error {
		if err := Publish(ctx, pool, runFailed(tenant)); err != nil {
			return err
		}
		if n := outboxRows(t, ctx, pool, tenant); n != 1 {
			t.Errorf("transaction holds %d outbox rows, want 1", n)
		}
		return nil
	})
}

func TestPostgres_PublishOnceRecordsOnce(t *testing.T) {
	pool := testPool(t)
	tenant := "test-" + uuid.NewString()
	inDiscardedTx(t, pool, func(ctx context.Context) error {
		for range 2 {
			if err := PublishOnce(ctx, pool, "budget|2026-11|80", runFailed(tenant)); err != nil {
				return err
			}
		}
		if n := outboxRows(t, ctx, pool, tenant); n != 1 {
			t.Errorf("repeated key wrote %d outbox rows, want 1", n)
		}
		return nil
	})
}
