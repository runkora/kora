package koraevents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// Postgres tests for the relay, the read queries and the retention sweep.
// They run when KORA_TEST_DB_URL points at a migrated database. Each test
// writes under its own tenant and deletes its rows.
func testStore(t *testing.T) (*Postgres, *storage.Pool, *pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run kora-events Postgres tests")
	}
	ctx := context.Background()
	raw, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	tenant := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = raw.Exec(ctx, `DELETE FROM event_outbox WHERE tenant_id = $1`, tenant)
		_, _ = raw.Exec(ctx, `DELETE FROM events WHERE tenant_id = $1`, tenant)
		raw.Close()
	})
	store := NewPostgres(raw)
	if err := store.EnsureRelayState(ctx); err != nil {
		t.Fatal(err)
	}
	return store, storage.NewPool(raw), raw, tenant
}

func event(tenant string, typ events.Type, agent string) events.Event {
	return events.Event{Type: typ, Tenant: tenant, Subject: "runs/" + uuid.NewString(), RunID: uuid.NewString(), Agent: agent, Entity: map[string]string{"state": "x"}}
}

func publish(t *testing.T, pool *storage.Pool, e events.Event) {
	t.Helper()
	if err := events.Publish(context.Background(), pool, e); err != nil {
		t.Fatal(err)
	}
}

func drain(t *testing.T, store *Postgres) {
	t.Helper()
	for {
		result, err := store.RelayBatch(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if result.Taken < relayBatchSize {
			return
		}
	}
}

func tenantSeqs(t *testing.T, raw *pgxpool.Pool, tenant string) []int64 {
	t.Helper()
	rows, err := raw.Query(context.Background(), `SELECT seq FROM events WHERE tenant_id = $1 ORDER BY seq`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	seqs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		t.Fatal(err)
	}
	return seqs
}

func TestPostgres_RelayGaplessUnderConcurrency(t *testing.T) {
	store, pool, raw, tenant := testStore(t)
	ctx := context.Background()
	var start int64
	if err := raw.QueryRow(ctx, `SELECT last_seq FROM event_relay_state`).Scan(&start); err != nil {
		t.Fatal(err)
	}

	const producers, perProducer = 8, 25
	stop := make(chan struct{})
	var relays sync.WaitGroup
	for range 2 {
		relays.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := store.RelayBatch(ctx); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	var writers sync.WaitGroup
	for range producers {
		writers.Go(func() {
			for range perProducer {
				err := storage.WithTransaction(ctx, pool, func(ctx context.Context, _ pgx.Tx) error {
					return events.Publish(ctx, pool, event(tenant, events.RunFailed, "a"))
				})
				if err != nil {
					t.Error(err)
				}
			}
		})
	}
	writers.Wait()
	close(stop)
	relays.Wait()
	drain(t, store)

	if seqs := tenantSeqs(t, raw, tenant); len(seqs) != producers*perProducer {
		t.Fatalf("relayed %d events, want %d", len(seqs), producers*perProducer)
	}
	var lowest, highest, count int64
	if err := raw.QueryRow(ctx, `SELECT min(seq), max(seq), count(*) FROM events WHERE seq > $1`, start).Scan(&lowest, &highest, &count); err != nil {
		t.Fatal(err)
	}
	if lowest != start+1 || count != highest-lowest+1 {
		t.Errorf("seq after %d runs %d..%d with %d events, want no gap", start, lowest, highest, count)
	}
}

func TestPostgres_RelayLateCommit(t *testing.T) {
	store, pool, raw, tenant := testStore(t)
	ctx := context.Background()

	published, release, done := make(chan struct{}), make(chan struct{}), make(chan error)
	go func() {
		done <- storage.WithTransaction(ctx, pool, func(ctx context.Context, _ pgx.Tx) error {
			if err := events.Publish(ctx, pool, event(tenant, events.RunStarted, "late")); err != nil {
				return err
			}
			close(published)
			<-release
			return nil
		})
	}()
	<-published
	publish(t, pool, event(tenant, events.RunFailed, "early"))
	drain(t, store)
	if seqs := tenantSeqs(t, raw, tenant); len(seqs) != 1 {
		t.Fatalf("before the late commit the log holds %d events, want 1", len(seqs))
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	drain(t, store)
	var early, late int64
	err := raw.QueryRow(ctx, `
		SELECT max(seq) FILTER (WHERE agent = 'early'), max(seq) FILTER (WHERE agent = 'late')
		FROM events WHERE tenant_id = $1`, tenant).Scan(&early, &late)
	if err != nil {
		t.Fatal(err)
	}
	if late <= early {
		t.Errorf("late row seq %d, early row seq %d: want the late row relayed after", late, early)
	}
}

func TestPostgres_RelayDropsRepeatedID(t *testing.T) {
	store, pool, raw, tenant := testStore(t)
	ctx := context.Background()
	e := event(tenant, events.BudgetExceeded, "")
	for range 2 {
		if err := events.PublishOnce(ctx, pool, "budget|2026-11", e); err != nil {
			t.Fatal(err)
		}
		drain(t, store)
	}
	if seqs := tenantSeqs(t, raw, tenant); len(seqs) != 1 {
		t.Errorf("repeated id recorded %d events, want 1", len(seqs))
	}
	var left int
	if err := raw.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE tenant_id = $1`, tenant).Scan(&left); err != nil || left != 0 {
		t.Errorf("outbox keeps %d rows (%v), want 0", left, err)
	}
}

func TestPostgres_Reads(t *testing.T) {
	store, pool, _, tenant := testStore(t)
	ctx := context.Background()
	publish(t, pool, event(tenant, events.RunFailed, "triage"))
	publish(t, pool, event(tenant, events.RunStarted, "triage"))
	publish(t, pool, event(tenant, events.GrantCreated, ""))
	publish(t, pool, event("other-"+tenant, events.RunFailed, "triage"))
	drain(t, store)
	t.Cleanup(func() { _, _ = store.pool.Exec(ctx, `DELETE FROM events WHERE tenant_id = $1`, "other-"+tenant) })

	all, err := store.List(ctx, ListQuery{Filter: Filter{Tenant: tenant}, Limit: 10})
	if err != nil || len(all) != 3 || all[0].Seq < all[2].Seq {
		t.Fatalf("List = %d events (%v), want 3 newest first", len(all), err)
	}
	runs, _ := store.List(ctx, ListQuery{Filter: Filter{Tenant: tenant, Categories: []string{"agents.run"}, Agents: []string{"triage"}}, Limit: 10})
	if len(runs) != 2 {
		t.Errorf("category and agent filter = %d events, want 2", len(runs))
	}
	older, _ := store.List(ctx, ListQuery{Filter: Filter{Tenant: tenant}, Before: Bound{Seq: all[0].Seq}, Limit: 1})
	if len(older) != 1 || older[0].Seq != all[1].Seq {
		t.Errorf("before seq %d = %v, want seq %d", all[0].Seq, older, all[1].Seq)
	}

	both, _ := store.List(ctx, ListQuery{Filter: Filter{Tenant: tenant, Categories: []string{"agents.run", "policy.grant"}}, Limit: 10})
	if len(both) != 3 {
		t.Errorf("two categories = %d events, want 3", len(both))
	}
	either, _ := store.List(ctx, ListQuery{Filter: Filter{Tenant: tenant, Types: []string{string(events.RunFailed), string(events.GrantCreated)}}, Limit: 10})
	if len(either) != 2 {
		t.Errorf("two types = %d events, want 2", len(either))
	}

	after, _ := store.After(ctx, Filter{Tenant: tenant}, all[2].Seq)
	if len(after) != 2 || after[0].Seq != all[1].Seq {
		t.Errorf("After = %v, want the two newer events oldest first", after)
	}

	var env events.Envelope
	got, err := store.Get(ctx, tenant, eventID(t, all[0], &env))
	if err != nil || got.Seq != all[0].Seq {
		t.Errorf("Get = %v %v", got.Seq, err)
	}
	if _, err := store.Get(ctx, "other-"+tenant, env.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get from another tenant = %v, want ErrNotFound", err)
	}
}

func eventID(t *testing.T, r Record, env *events.Envelope) string {
	t.Helper()
	if err := json.Unmarshal(r.Event, env); err != nil {
		t.Fatal(err)
	}
	return env.ID
}

func TestPostgres_SweepBatch(t *testing.T) {
	store, pool, raw, tenant := testStore(t)
	ctx := context.Background()
	for range 3 {
		publish(t, pool, event(tenant, events.RunFailed, ""))
	}
	drain(t, store)
	seqs := tenantSeqs(t, raw, tenant)
	// One event inside the window, one outside it, one before 2000 for the default scope.
	if _, err := raw.Exec(ctx, `UPDATE events SET recorded_at = now() - interval '40 days' WHERE seq = $1`, seqs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(ctx, `UPDATE events SET recorded_at = '1999-01-01' WHERE seq = $1`, seqs[1]); err != nil {
		t.Fatal(err)
	}

	deleted, locked, err := store.SweepBatch(ctx, SweepScope{Tenant: tenant}, time.Now().AddDate(0, 0, -30))
	if err != nil || !locked || deleted != 2 {
		t.Fatalf("tenant sweep deleted %d (locked %v, %v), want 2", deleted, locked, err)
	}
	publish(t, pool, event(tenant, events.RunFailed, ""))
	drain(t, store)
	if _, err := raw.Exec(ctx, `UPDATE events SET recorded_at = '1999-01-01' WHERE tenant_id = $1`, tenant); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if deleted, _, _ := store.SweepBatch(ctx, SweepScope{Except: []string{tenant}}, cutoff); deleted != 0 {
		t.Errorf("default sweep deleted %d events of an excepted tenant", deleted)
	}
	if deleted, _, _ := store.SweepBatch(ctx, SweepScope{Except: []string{}}, cutoff); deleted != 2 {
		t.Errorf("default sweep deleted %d events, want 2", deleted)
	}
}

func TestPostgres_Search(t *testing.T) {
	store, pool, _, tenant := testStore(t)
	ctx := context.Background()
	plain := event(tenant, events.RunFailed, "Triage-Bot")
	wild := event(tenant, events.RunStarted, "load_100%")
	other := event(tenant, events.GrantCreated, "")
	other.Subject = "grants/g1"
	for _, e := range []events.Event{plain, wild, other} {
		publish(t, pool, e)
	}
	drain(t, store)

	for _, tc := range []struct {
		q    string
		want int
	}{
		{"triage-bot", 1},
		{"RUN.", 2},
		{"grants/", 1},
		{plain.RunID[:8], 1},
		{"100%", 1},
		{"%", 1},
		{"_", 1},
		{`\`, 0},
		{"absent", 0},
	} {
		got, err := store.List(ctx, ListQuery{Filter: Filter{Tenant: tenant}, Q: tc.q, Limit: 10})
		if err != nil || len(got) != tc.want {
			t.Errorf("q %q = %d events (%v), want %d", tc.q, len(got), err, tc.want)
		}
	}
}

func TestPostgres_RelayDeadLettersMalformedRows(t *testing.T) {
	store, pool, raw, tenant := testStore(t)
	ctx := context.Background()
	bad := map[string]string{
		uuid.NewString(): `"not an envelope"`,
		uuid.NewString(): `{"koratenant":"other","time":"2026-11-06T00:00:00Z","subject":"s","koraprincipal":"p","koraprincipaltype":"SYSTEM"}`,
		uuid.NewString(): `{"koratenant":"` + tenant + `","time":"2026-11-06T00:00:00Z","subject":"s","koraprincipal":"p","koraprincipaltype":"SYSTEM","korarun":"run-1"}`,
	}
	for id, env := range bad {
		if _, err := raw.Exec(ctx, `INSERT INTO event_outbox (id, tenant_id, type, envelope) VALUES ($1, $2, 'agents.run.failed.v1', $3::text::jsonb)`, id, tenant, env); err != nil {
			t.Fatal(err)
		}
	}
	publish(t, pool, event(tenant, events.RunFailed, "good"))

	t.Cleanup(func() { _, _ = raw.Exec(ctx, `DELETE FROM event_outbox_dead WHERE tenant_id = $1`, tenant) })
	// A running kora-events may relay these rows first. The dead-letter table is the record.
	drain(t, store)
	for id, env := range bad {
		var kept, reason string
		err := raw.QueryRow(ctx, `SELECT envelope::text, reason FROM event_outbox_dead WHERE id = $1`, id).Scan(&kept, &reason)
		if err != nil || reason == "" {
			t.Errorf("malformed row %s: dead-letter row %v, reason %q", id, err, reason)
			continue
		}
		var a, b any
		_ = json.Unmarshal([]byte(kept), &a)
		_ = json.Unmarshal([]byte(env), &b)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("dead-letter row %s holds %s, want the original %s", id, kept, env)
		}
	}
	if seqs := tenantSeqs(t, raw, tenant); len(seqs) != 1 {
		t.Errorf("log holds %d events of the tenant, want only the good one", len(seqs))
	}
}

func TestPostgres_SweepDeadLetters(t *testing.T) {
	store, _, raw, tenant := testStore(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = raw.Exec(ctx, `DELETE FROM event_outbox_dead WHERE tenant_id = ANY($1)`, []string{tenant, ""})
	})
	unknown := ""
	ids := map[string]string{uuid.NewString(): tenant, uuid.NewString(): tenant, uuid.NewString(): unknown}
	old := ""
	for id, owner := range ids {
		if old == "" && owner == tenant {
			old = id
		}
		if _, err := raw.Exec(ctx, `
			INSERT INTO event_outbox_dead (id, position, tenant_id, type, envelope, created_at, reason, dead_at)
			VALUES ($1, 0, $2, 'agents.run.failed.v1', '{}', now(), 'test', '1999-01-01')`, id, owner); err != nil {
			t.Fatal(err)
		}
	}
	// One of the tenant's rows is recent and stays.
	if _, err := raw.Exec(ctx, `UPDATE event_outbox_dead SET dead_at = now() WHERE id = $1`, old); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if deleted, _, err := store.SweepBatch(ctx, SweepScope{Tenant: tenant, Dead: true}, cutoff); err != nil || deleted != 1 {
		t.Errorf("tenant dead-letter sweep deleted %d (%v), want 1", deleted, err)
	}
	if deleted, _, err := store.SweepBatch(ctx, SweepScope{Except: []string{tenant}, Dead: true}, cutoff); err != nil || deleted != 1 {
		t.Errorf("default dead-letter sweep deleted %d (%v), want the row with no tenant", deleted, err)
	}
	var left int
	if err := raw.QueryRow(ctx, `SELECT count(*) FROM event_outbox_dead WHERE id = ANY($1::uuid[])`, keys(ids)).Scan(&left); err != nil || left != 1 {
		t.Errorf("%d dead-letter rows left (%v), want the recent one", left, err)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
