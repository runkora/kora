package koraevents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

const (
	// relayBatchSize is the most outbox rows one relay transaction moves.
	relayBatchSize = 500
	// sweepBatchSize is the most events one retention transaction deletes.
	sweepBatchSize = 5000
	// readBatchSize is the most events one stream read returns.
	readBatchSize = 500
)

// recordColumns are the columns scanRecord reads, in order.
const recordColumns = `seq, recorded_at, envelope, tenant_id, category, type, COALESCE(agent, ''), principal, COALESCE(run_id::text, '')`

// Postgres reads and writes the kora-events tables.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres returns the store on pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

// EnsureRelayState inserts the relay's single row when it is missing.
func (p *Postgres) EnsureRelayState(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `INSERT INTO event_relay_state DEFAULT VALUES ON CONFLICT DO NOTHING`); err != nil {
		return fmt.Errorf("insert relay state: %w", err)
	}
	return nil
}

// RelayBatch moves up to relayBatchSize outbox rows into the log in one
// transaction and returns how many rows it took from the outbox. Only the
// holder of the relay lock assigns seq, which keeps seq gapless.
func (p *Postgres) RelayBatch(ctx context.Context) (int, error) {
	taken := 0
	err := storage.WithTransaction(ctx, p.pool, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('kora_events_relay'))`); err != nil {
			return fmt.Errorf("take relay lock: %w", err)
		}
		var last int64
		if err := tx.QueryRow(ctx, `SELECT last_seq FROM event_relay_state WHERE id`).Scan(&last); err != nil {
			return fmt.Errorf("read relay state: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT o.id::text, o.envelope, EXISTS (SELECT 1 FROM events e WHERE e.id = o.id)
			FROM   event_outbox o
			ORDER  BY o.position
			LIMIT  $1`, relayBatchSize)
		if err != nil {
			return fmt.Errorf("read outbox: %w", err)
		}
		type outboxRow struct {
			id       string
			envelope []byte
			recorded bool
		}
		outbox, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (outboxRow, error) {
			var r outboxRow
			return r, row.Scan(&r.id, &r.envelope, &r.recorded)
		})
		if err != nil {
			return fmt.Errorf("scan outbox: %w", err)
		}
		if len(outbox) == 0 {
			return nil
		}

		ids := make([]string, 0, len(outbox))
		batch := &pgx.Batch{}
		for _, r := range outbox {
			ids = append(ids, r.id)
			if r.recorded {
				continue
			}
			var env events.Envelope
			if err := json.Unmarshal(r.envelope, &env); err != nil {
				return fmt.Errorf("decode outbox row %s: %w", r.id, err)
			}
			occurred, err := time.Parse(time.RFC3339Nano, env.Time)
			if err != nil {
				return fmt.Errorf("outbox row %s time: %w", r.id, err)
			}
			last++
			batch.Queue(`
				INSERT INTO events (seq, id, tenant_id, type, category, subject, principal, principal_type,
				                    run_id, agent, caused_by, chain_depth, occurred_at, envelope)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
				last, r.id, env.Tenant, string(env.Type), env.Type.Category(), env.Subject, env.Principal, env.PrincipalType,
				storage.NullIfZero(env.Run), storage.NullIfZero(env.Agent), storage.NullIfZero(env.CausedBy),
				env.ChainDepth, occurred, r.envelope)
		}
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return fmt.Errorf("insert events: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM event_outbox WHERE id = ANY($1::uuid[])`, ids); err != nil {
			return fmt.Errorf("delete outbox rows: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE event_relay_state SET last_seq = $1 WHERE id`, last); err != nil {
			return fmt.Errorf("update relay state: %w", err)
		}
		if _, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, LogChannel, strconv.FormatInt(last, 10)); err != nil {
			return fmt.Errorf("notify %s: %w", LogChannel, err)
		}
		taken = len(outbox)
		return nil
	})
	return taken, err
}

// List returns one page of the filtered events, newest first.
func (p *Postgres) List(ctx context.Context, q ListQuery) ([]Record, error) {
	where, args := filterClause(q.Filter)
	add := func(clause string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND "+clause, len(args))
	}
	if q.Before.Seq > 0 {
		add("seq < $%d", q.Before.Seq)
	}
	if !q.Before.Time.IsZero() {
		add("occurred_at < $%d", q.Before.Time)
	}
	if q.After.Seq > 0 {
		add("seq > $%d", q.After.Seq)
	}
	if !q.After.Time.IsZero() {
		add("occurred_at > $%d", q.After.Time)
	}
	args = append(args, q.Limit)
	return p.query(ctx, fmt.Sprintf(`SELECT %s FROM events WHERE %s ORDER BY seq DESC LIMIT $%d`, recordColumns, where, len(args)), args...)
}

// After returns up to readBatchSize filtered events after seq, oldest first.
func (p *Postgres) After(ctx context.Context, f Filter, seq int64) ([]Record, error) {
	where, args := filterClause(f)
	args = append(args, seq, readBatchSize)
	return p.query(ctx, fmt.Sprintf(`SELECT %s FROM events WHERE %s AND seq > $%d ORDER BY seq LIMIT $%d`,
		recordColumns, where, len(args)-1, len(args)), args...)
}

// Get returns the event with id in tenant.
func (p *Postgres) Get(ctx context.Context, tenant, id string) (Record, error) {
	records, err := p.query(ctx, `SELECT `+recordColumns+` FROM events WHERE tenant_id = $1 AND id = $2`, tenant, id)
	if err != nil {
		return Record{}, err
	}
	if len(records) == 0 {
		return Record{}, ErrNotFound
	}
	return records[0], nil
}

// LatestSeq returns the highest seq in the log, or 0.
func (p *Postgres) LatestSeq(ctx context.Context) (int64, error) {
	var seq int64
	if err := p.pool.QueryRow(ctx, `SELECT COALESCE(MAX(seq), 0) FROM events`).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read latest seq: %w", err)
	}
	return seq, nil
}

// SweepScope selects the events one retention batch may delete: one tenant's,
// or every tenant's except those listed.
type SweepScope struct {
	Tenant string
	Except []string
}

// SweepBatch deletes up to sweepBatchSize events recorded before cutoff and
// returns how many it deleted. It deletes nothing when another replica holds
// the retention lock, and reports that as false.
func (p *Postgres) SweepBatch(ctx context.Context, scope SweepScope, cutoff time.Time) (int64, bool, error) {
	var deleted int64
	locked := false
	err := storage.WithTransaction(ctx, p.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('kora_events_retention'))`).Scan(&locked); err != nil {
			return fmt.Errorf("take retention lock: %w", err)
		}
		if !locked {
			return nil
		}
		match, arg := `tenant_id = $3`, any(scope.Tenant)
		if scope.Tenant == "" {
			// A nil slice encodes as NULL, and tenant_id <> ALL(NULL) matches no row.
			match, arg = `tenant_id <> ALL($3::text[])`, append([]string{}, scope.Except...)
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM events WHERE seq IN (
				SELECT seq FROM events WHERE recorded_at < $1 AND `+match+` ORDER BY seq LIMIT $2
			)`, cutoff, sweepBatchSize, arg)
		if err != nil {
			return fmt.Errorf("delete expired events: %w", err)
		}
		deleted = tag.RowsAffected()
		return nil
	})
	return deleted, locked, err
}

func (p *Postgres) query(ctx context.Context, sql string, args ...any) ([]Record, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	records, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Record, error) {
		var r Record
		return r, row.Scan(&r.Seq, &r.RecordedAt, &r.Event, &r.tenant, &r.category, &r.eventType, &r.agent, &r.principal, &r.runID)
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("scan events: %w", err)
	}
	return records, nil
}

// filterClause returns the WHERE clause and arguments of f.
func filterClause(f Filter) (string, []any) {
	clauses := []string{"TRUE"}
	var args []any
	for _, c := range []struct {
		clause, value string
	}{
		{"tenant_id = $%d", f.Tenant},
		{"category = $%d", f.Category},
		{"type = $%d", f.Type},
		{"agent = $%d", f.Agent},
		{"principal = $%d", f.Principal},
		{"run_id = $%d::uuid", f.RunID},
	} {
		if c.value != "" {
			args = append(args, c.value)
			clauses = append(clauses, fmt.Sprintf(c.clause, len(args)))
		}
	}
	return strings.Join(clauses, " AND "), args
}
