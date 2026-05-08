package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a state transition is invalid (CAS mismatch).
var ErrConflict = errors.New("conflict")

// ErrSlugTaken is returned when a slug is already in use within a tenant.
var ErrSlugTaken = errors.New("slug already taken")

// PostgresRepository implements Repository against a pgxpool.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a repository backed by the given pool.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// ── Agent Types ───────────────────────────────────────────────────────────────

func (r *PostgresRepository) CreateAgentType(ctx context.Context, at AgentType) error {
	if at.ID == "" {
		at.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO agent_types
			(id, tenant_id, name, slug, tool_scopes, max_concurrent_runs_per_pod, state, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		at.ID, at.TenantID, at.Name, at.Slug, at.ToolScopes,
		at.MaxConcurrentRunsPerPod, string(at.State), now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrSlugTaken
		}
		return fmt.Errorf("insert agent_type: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetAgentType(ctx context.Context, id string) (*AgentType, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, slug, tool_scopes, max_concurrent_runs_per_pod, state, created_at, updated_at
		FROM agent_types WHERE id = $1`, id)
	return scanAgentType(row)
}

func (r *PostgresRepository) GetAgentTypeBySlug(ctx context.Context, tenantID, slug string) (*AgentType, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, slug, tool_scopes, max_concurrent_runs_per_pod, state, created_at, updated_at
		FROM agent_types WHERE tenant_id = $1 AND slug = $2`, tenantID, slug)
	return scanAgentType(row)
}

func (r *PostgresRepository) ListAgentTypes(ctx context.Context, tenantID string) ([]AgentType, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, slug, tool_scopes, max_concurrent_runs_per_pod, state, created_at, updated_at
		FROM agent_types WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query agent_types: %w", err)
	}
	defer rows.Close()

	var out []AgentType
	for rows.Next() {
		at, err := scanAgentType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *at)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpdateAgentType(ctx context.Context, at AgentType) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE agent_types
		SET name=$1, slug=$2, tool_scopes=$3, max_concurrent_runs_per_pod=$4, updated_at=$5
		WHERE id=$6`,
		at.Name, at.Slug, at.ToolScopes, at.MaxConcurrentRunsPerPod, time.Now().UTC(), at.ID,
	)
	if err != nil {
		return fmt.Errorf("update agent_type: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) SetAgentTypeState(ctx context.Context, id string, from, to AgentTypeState) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE agent_types SET state=$1, updated_at=$2
		WHERE id=$3 AND state=$4`,
		string(to), time.Now().UTC(), id, string(from),
	)
	if err != nil {
		return fmt.Errorf("set agent_type state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *PostgresRepository) DeleteAgentType(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM agent_types WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete agent_type: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ── Runs ──────────────────────────────────────────────────────────────────────

func (r *PostgresRepository) CreateRun(ctx context.Context, run Run) error {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO runs
			(id, tenant_id, agent_type_id, state, pod_ordinal, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		run.ID, run.TenantID, run.AgentTypeID, string(RunStatePending), -1, run.CreatedBy, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetRun(ctx context.Context, id string) (*Run, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, agent_type_id, state, pod_ordinal, created_by, started_at, completed_at, created_at, updated_at
		FROM runs WHERE id=$1`, id)
	return scanRun(row)
}

func (r *PostgresRepository) ListRuns(ctx context.Context, tenantID, agentTypeID string) ([]Run, error) {
	var rows pgx.Rows
	var err error
	if agentTypeID != "" {
		rows, err = r.pool.Query(ctx, `
			SELECT id, tenant_id, agent_type_id, state, pod_ordinal, created_by, started_at, completed_at, created_at, updated_at
			FROM runs WHERE tenant_id=$1 AND agent_type_id=$2 ORDER BY created_at DESC`, tenantID, agentTypeID)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, tenant_id, agent_type_id, state, pod_ordinal, created_by, started_at, completed_at, created_at, updated_at
			FROM runs WHERE tenant_id=$1 ORDER BY created_at DESC`, tenantID)
	}
	if err != nil {
		return nil, fmt.Errorf("query runs: %w", err)
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SetRunState(ctx context.Context, id string, from, to RunState) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE runs SET state=$1, updated_at=$2
		WHERE id=$3 AND state=$4`,
		string(to), time.Now().UTC(), id, string(from),
	)
	if err != nil {
		return fmt.Errorf("set run state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *PostgresRepository) AssignPod(ctx context.Context, runID string, podOrdinal int) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE runs SET state=$1, pod_ordinal=$2, started_at=$3, updated_at=$3
		WHERE id=$4 AND state=$5`,
		string(RunStateStarting), podOrdinal, now, runID, string(RunStatePending),
	)
	if err != nil {
		return fmt.Errorf("assign pod: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *PostgresRepository) DeleteRun(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM runs WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ActiveRunsPerPod(ctx context.Context, agentTypeID string) (map[int]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT pod_ordinal, COUNT(*) FROM runs
		WHERE agent_type_id=$1 AND state IN ('pending','starting','active','completing')
		GROUP BY pod_ordinal`, agentTypeID)
	if err != nil {
		return nil, fmt.Errorf("query active runs per pod: %w", err)
	}
	defer rows.Close()

	out := make(map[int]int)
	for rows.Next() {
		var ord, count int
		if err := rows.Scan(&ord, &count); err != nil {
			return nil, err
		}
		out[ord] = count
	}
	return out, rows.Err()
}

// ── Events ────────────────────────────────────────────────────────────────────

func (r *PostgresRepository) AppendRunEvent(ctx context.Context, e RunEvent) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO run_events (run_id, event_type, payload, created_at)
		VALUES ($1,$2,$3,$4)`,
		e.RunID, e.EventType, e.Payload, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert run_event: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ListRunEvents(ctx context.Context, runID string, afterSeqNo int64) ([]RunEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT sequence_no, run_id, event_type, payload, created_at
		FROM run_events WHERE run_id=$1 AND sequence_no > $2
		ORDER BY sequence_no ASC`, runID, afterSeqNo)
	if err != nil {
		return nil, fmt.Errorf("query run_events: %w", err)
	}
	defer rows.Close()

	var out []RunEvent
	for rows.Next() {
		var e RunEvent
		if err := rows.Scan(&e.SequenceNo, &e.RunID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ── Messages ──────────────────────────────────────────────────────────────────

func (r *PostgresRepository) AppendRunMessage(ctx context.Context, m RunMessage) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO run_messages (id, run_id, content, created_at)
		VALUES ($1,$2,$3,$4)`,
		m.ID, m.RunID, m.Content, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert run_message: %w", err)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(...any) error
}

func scanAgentType(s scanner) (*AgentType, error) {
	var at AgentType
	var state string
	err := s.Scan(
		&at.ID, &at.TenantID, &at.Name, &at.Slug, &at.ToolScopes,
		&at.MaxConcurrentRunsPerPod, &state, &at.CreatedAt, &at.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent_type: %w", err)
	}
	at.State = AgentTypeState(state)
	return &at, nil
}

func scanRun(s scanner) (*Run, error) {
	var r Run
	var state string
	err := s.Scan(
		&r.ID, &r.TenantID, &r.AgentTypeID, &state, &r.PodOrdinal,
		&r.CreatedBy, &r.StartedAt, &r.CompletedAt, &r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan run: %w", err)
	}
	r.State = RunState(state)
	return &r, nil
}

func isUniqueViolation(err error) bool {
	// pgx wraps pgconn.PgError; check the SQLSTATE code for unique_violation (23505).
	type pgErr interface{ SQLState() string }
	var pe pgErr
	if errors.As(err, &pe) {
		return pe.SQLState() == "23505"
	}
	return false
}
