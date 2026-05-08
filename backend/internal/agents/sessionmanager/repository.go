package sessionmanager

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// eventRepository writes durable Orbit events to the run_events table.
// Only message_complete, tool_use, and tool_result are written here — ephemeral
// streaming events (text_delta, thinking_delta, message_stop) are push-only (ADR-0054).
type eventRepository struct {
	pool *pgxpool.Pool
}

func newEventRepository(pool *pgxpool.Pool) *eventRepository {
	return &eventRepository{pool: pool}
}

// Write inserts a durable event row into run_events.
func (r *eventRepository) Write(ctx context.Context, runID, eventType string, payload json.RawMessage) error {
	const insertSQL = `
		INSERT INTO run_events (run_id, event_type, payload, created_at)
		VALUES ($1, $2, $3, now())
	`
	if _, err := r.pool.Exec(ctx, insertSQL, runID, eventType, payload); err != nil {
		return fmt.Errorf("insert run_event: %w", err)
	}
	return nil
}
