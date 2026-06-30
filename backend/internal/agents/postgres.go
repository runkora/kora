package agents

import (
	"context"
	"encoding/json"
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

// ErrInvalidConnectivity is returned when an agent type's connectivity posture
// is malformed (bad mode or whitelist host). Handlers map it to a 400.
var ErrInvalidConnectivity = errors.New("invalid connectivity")

// ErrInvalidConnector is returned when an agent type's connector attachment is
// malformed (empty connection name or action). Handlers map it to a 400.
var ErrInvalidConnector = errors.New("invalid connector")

// ErrRunNotAcceptingMessages is returned when a message is sent to a run that is not in a state that accepts messages.
var ErrRunNotAcceptingMessages = errors.New("run not accepting messages")

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
	connectivityJSON, mErr := json.Marshal(at.Connectivity)
	if mErr != nil {
		return fmt.Errorf("marshal connectivity: %w", mErr)
	}
	connectorsJSON, mErr := json.Marshal(at.Connectors)
	if mErr != nil {
		return fmt.Errorf("marshal connectors: %w", mErr)
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO agent_types
			(id, tenant_id, name, slug, description, system_prompt, framework, tool_scopes, grant_scopes, max_concurrent_runs_per_pod, state, model_id, created_at, updated_at, connectivity, connectors, environment_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,$16::jsonb,$17)`,
		at.ID, at.TenantID, at.Name, at.Slug, at.Description, at.SystemPrompt, string(at.Framework),
		at.ToolScopes, at.GrantScopes, at.MaxConcurrentRunsPerPod, string(at.State), at.ModelID, now, now, string(connectivityJSON), string(connectorsJSON), at.EnvironmentID,
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
		SELECT id, tenant_id, name, slug, description, system_prompt, framework, tool_scopes, grant_scopes, max_concurrent_runs_per_pod, state, model_id, created_at, updated_at, connectivity, connectors, environment_id
		FROM agent_types WHERE id = $1`, id)
	return scanAgentType(row)
}

func (r *PostgresRepository) GetAgentTypeBySlug(ctx context.Context, tenantID, slug string) (*AgentType, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, slug, description, system_prompt, framework, tool_scopes, grant_scopes, max_concurrent_runs_per_pod, state, model_id, created_at, updated_at, connectivity, connectors, environment_id
		FROM agent_types WHERE tenant_id = $1 AND slug = $2`, tenantID, slug)
	return scanAgentType(row)
}

func (r *PostgresRepository) ListAgentTypes(ctx context.Context, tenantID string) ([]AgentType, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, slug, description, system_prompt, framework, tool_scopes, grant_scopes, max_concurrent_runs_per_pod, state, model_id, created_at, updated_at, connectivity, connectors, environment_id
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
	connectivityJSON, mErr := json.Marshal(at.Connectivity)
	if mErr != nil {
		return fmt.Errorf("marshal connectivity: %w", mErr)
	}
	connectorsJSON, mErr := json.Marshal(at.Connectors)
	if mErr != nil {
		return fmt.Errorf("marshal connectors: %w", mErr)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE agent_types
		SET name=$1, slug=$2, description=$3, system_prompt=$4, framework=$5, tool_scopes=$6, grant_scopes=$7, max_concurrent_runs_per_pod=$8, model_id=$9, updated_at=$10, connectivity=$11::jsonb, connectors=$12::jsonb, environment_id=$13
		WHERE id=$14`,
		at.Name, at.Slug, at.Description, at.SystemPrompt, string(at.Framework), at.ToolScopes, at.GrantScopes, at.MaxConcurrentRunsPerPod, at.ModelID, time.Now().UTC(), string(connectivityJSON), string(connectorsJSON), at.EnvironmentID, at.ID,
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

func (r *PostgresRepository) ListAllAgentTypeSlugs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT slug FROM agent_types ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("query agent_type slugs: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
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
		SELECT id, tenant_id, agent_type_id, state, pod_ordinal, created_by, started_at, completed_at, created_at, updated_at, summary
		FROM runs WHERE id=$1`, id)
	return scanRun(row)
}

func (r *PostgresRepository) ListRuns(ctx context.Context, tenantID, agentTypeID string) ([]Run, error) {
	var rows pgx.Rows
	var err error
	if agentTypeID != "" {
		rows, err = r.pool.Query(ctx, `
			SELECT id, tenant_id, agent_type_id, state, pod_ordinal, created_by, started_at, completed_at, created_at, updated_at, summary
			FROM runs WHERE tenant_id=$1 AND agent_type_id=$2 ORDER BY created_at DESC`, tenantID, agentTypeID)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, tenant_id, agent_type_id, state, pod_ordinal, created_by, started_at, completed_at, created_at, updated_at, summary
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
	now := time.Now().UTC()
	var tag interface{ RowsAffected() int64 }
	var err error
	if to == RunStateCompleted || to == RunStateFailed {
		tag, err = r.pool.Exec(ctx, `
			UPDATE runs SET state=$1, completed_at=$2, updated_at=$2
			WHERE id=$3 AND state=$4`,
			string(to), now, id, string(from),
		)
	} else {
		tag, err = r.pool.Exec(ctx, `
			UPDATE runs SET state=$1, updated_at=$2
			WHERE id=$3 AND state=$4`,
			string(to), now, id, string(from),
		)
	}
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

// MarkRunFailed unconditionally transitions a run to the failed terminal state and
// stamps completed_at. Unlike SetRunState it is not a compare-and-swap — it cleans
// up a run whose start failed from whatever non-terminal state it reached. Zero
// rows affected (run already gone) is not an error.
func (r *PostgresRepository) MarkRunFailed(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		UPDATE runs SET state=$1, completed_at=$2, updated_at=$2 WHERE id=$3`,
		string(RunStateFailed), now, id,
	)
	if err != nil {
		return fmt.Errorf("mark run failed: %w", err)
	}
	return nil
}

// SetRunSummary writes the task summary set-once: the first summary for a run
// wins. A resumed session's first message would otherwise re-summarize and
// clobber the original task label, so the UPDATE only fires while summary IS
// NULL. Zero rows affected (already set, or run gone) is not an error — the
// callback is best-effort and idempotent.
func (r *PostgresRepository) SetRunSummary(ctx context.Context, runID, summary string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE runs SET summary=$1, updated_at=$2 WHERE id=$3 AND summary IS NULL`,
		summary, time.Now().UTC(), runID,
	)
	if err != nil {
		return fmt.Errorf("set run summary: %w", err)
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

func (r *PostgresRepository) DeleteRunsByAgentType(ctx context.Context, agentTypeID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `DELETE FROM runs WHERE agent_type_id=$1 RETURNING id`, agentTypeID)
	if err != nil {
		return nil, fmt.Errorf("delete runs for agent type: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("scan deleted run id: %w", scanErr)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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

// ReapStaleRuns marks runs that have been stuck in transient states for longer
// than staleDuration. A run is only reaped when neither runs.updated_at NOR its
// most recent run_events row is newer than the cutoff — events act as a liveness
// heartbeat, so a long-but-active turn (which streams events continuously) is not
// reaped, while a run whose pod died (no events, no state changes) is. Returns the
// IDs partitioned by the terminal state each row was moved to, so the caller can
// emit the correct event type per group.
//
//   - active / starting → failed (session-manager died mid-turn)
//   - idle → completed  (session-manager died after a clean turn; idle timeout
//     never fired because the pod restarted)
func (r *PostgresRepository) ReapStaleRuns(ctx context.Context, staleDuration time.Duration) (failed, completed []string, err error) {
	now := time.Now().UTC()
	cutoff := now.Add(-staleDuration)
	rows, err := r.pool.Query(ctx, `
		UPDATE runs
		SET state = CASE
			WHEN state IN ('active','starting') THEN 'failed'
			ELSE 'completed'
		END,
		completed_at = $1,
		updated_at   = $1
		WHERE state IN ('active','starting','idle')
		  AND updated_at < $2
		  AND NOT EXISTS (
		      SELECT 1 FROM run_events e
		      WHERE e.run_id = runs.id AND e.created_at >= $2
		  )
		RETURNING id, state`,
		now, cutoff,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("reap stale runs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id    string
			state string
		)
		if scanErr := rows.Scan(&id, &state); scanErr != nil {
			return nil, nil, scanErr
		}
		switch state {
		case string(RunStateFailed):
			failed = append(failed, id)
		case string(RunStateCompleted):
			completed = append(completed, id)
		}
	}
	return failed, completed, rows.Err()
}

// ── Events ────────────────────────────────────────────────────────────────────

func (r *PostgresRepository) AppendRunEvent(ctx context.Context, e RunEvent) (int64, error) {
	var seqNo int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO run_events (run_id, event_type, payload, created_at)
		VALUES ($1,$2,$3,$4)
		RETURNING sequence_no`,
		e.RunID, e.EventType, e.Payload, time.Now().UTC(),
	).Scan(&seqNo)
	if err != nil {
		return 0, fmt.Errorf("insert run_event: %w", err)
	}
	return seqNo, nil
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
	var state, framework string
	var connectivityJSON, connectorsJSON []byte
	err := s.Scan(
		&at.ID, &at.TenantID, &at.Name, &at.Slug, &at.Description, &at.SystemPrompt, &framework,
		&at.ToolScopes, &at.GrantScopes, &at.MaxConcurrentRunsPerPod, &state, &at.ModelID, &at.CreatedAt, &at.UpdatedAt, &connectivityJSON, &connectorsJSON, &at.EnvironmentID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent_type: %w", err)
	}
	at.State = AgentTypeState(state)
	at.Framework = AgentFramework(framework)
	if len(connectivityJSON) > 0 {
		if uErr := json.Unmarshal(connectivityJSON, &at.Connectivity); uErr != nil {
			return nil, fmt.Errorf("unmarshal connectivity: %w", uErr)
		}
	}
	if len(connectorsJSON) > 0 {
		if uErr := json.Unmarshal(connectorsJSON, &at.Connectors); uErr != nil {
			return nil, fmt.Errorf("unmarshal connectors: %w", uErr)
		}
	}
	return &at, nil
}

func scanRun(s scanner) (*Run, error) {
	var r Run
	var state string
	err := s.Scan(
		&r.ID, &r.TenantID, &r.AgentTypeID, &state, &r.PodOrdinal,
		&r.CreatedBy, &r.StartedAt, &r.CompletedAt, &r.CreatedAt, &r.UpdatedAt,
		&r.Summary,
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
