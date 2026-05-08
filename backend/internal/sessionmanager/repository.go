package sessionmanager

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// eventRepository writes Orbit events to the run_events table.
// The session-manager owns writes for all CLI output events (text_delta,
// thinking_delta, tool_use, tool_result, message_stop). The run_state event
// is written exclusively by orbit-agents (ADR-0054).
type eventRepository struct {
	pool *pgxpool.Pool
}

func newEventRepository(pool *pgxpool.Pool) *eventRepository {
	return &eventRepository{pool: pool}
}

// Write inserts an event row and issues a Postgres NOTIFY so orbit-agents
// can relay it to SSE subscribers.
func (r *eventRepository) Write(ctx context.Context, runID, eventType string, payload json.RawMessage) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const insertSQL = `
		INSERT INTO run_events (run_id, event_type, payload, created_at)
		VALUES ($1, $2, $3, now())
		RETURNING sequence_no
	`
	var seqNo int64
	if err := tx.QueryRow(ctx, insertSQL, runID, eventType, payload).Scan(&seqNo); err != nil {
		return fmt.Errorf("insert run_event: %w", err)
	}

	channel := "run:" + runID
	if _, err := tx.Exec(ctx, "SELECT pg_notify($1, $2)", channel, fmt.Sprintf("%d", seqNo)); err != nil {
		return fmt.Errorf("notify: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
