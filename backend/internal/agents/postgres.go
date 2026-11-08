package agents

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sandboxfolder "github.com/runkora/kora/backend/internal/agents/sandbox/folder"
	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/storage"
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

// ErrInvalidIdentityMode is returned when an agent type's identity mode is not
// autonomous or delegated. Handlers map it to a 400.
var ErrInvalidIdentityMode = errors.New("invalid identity mode")

// ErrInvalidFramework is returned when an agent type's framework is not one of
// the supported harnesses (internal/platform/harnesses).
var ErrInvalidFramework = errors.New("invalid framework")

// ErrIdentityModeImmutable is returned when an update attempts to change an
// agent type's identity mode — it is fixed at creation
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 1). Handlers map it to a 400.
var ErrIdentityModeImmutable = errors.New("identity_mode is immutable: it is set at creation and cannot be changed")

// ErrRunNotAcceptingMessages is returned when a message is sent to a run that is not in a state that accepts messages.
var ErrRunNotAcceptingMessages = errors.New("run not accepting messages")

// ErrRunStarting is returned when a message is sent to a run the scheduler has not started yet.
var ErrRunStarting = errors.New("run is starting")

// PostgresRepository implements Repository. Its writes record their spine events in the same transaction.
type PostgresRepository struct {
	pool *storage.Pool
}

// NewPostgresRepository creates a repository backed by the given pool.
func NewPostgresRepository(pool *storage.Pool) *PostgresRepository {
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
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO agent_types
				(id, tenant_id, name, slug, description, system_prompt, framework, grant_scopes, max_concurrent_runs_per_pod, state, model_id, created_at, updated_at, connectivity, connectors, mcp_tools, identity_mode, guardrails, created_by, source, resource_uid, resource_generation, repository)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15::jsonb,$16,$17,$18::jsonb,$19,$20,NULLIF($21, ''),$22,NULLIF($23, ''))`,
			at.ID, at.TenantID, at.Name, at.Slug, at.Description, at.SystemPrompt, string(at.Framework),
			at.GrantScopes, at.MaxConcurrentRunsPerPod, string(at.State), at.ModelID, now, now, string(connectivityJSON), string(connectorsJSON), at.MCPTools, string(at.IdentityMode.OrDefault()),
			guardrailsColumn(at.Guardrails), at.CreatedBy, cmp.Or(at.Source, declarative.SourceAPI), at.ResourceUID, at.ResourceGeneration, at.Repository,
		)
		if err != nil {
			if isUniqueViolation(err) {
				return ErrSlugTaken
			}
			return fmt.Errorf("insert agent_type: %w", err)
		}
		if err = writeAgentEnvironments(ctx, tx, at.ID, at.Environments); err != nil {
			return err
		}
		created, err := r.GetAgentType(ctx, at.ID)
		if err != nil {
			return err
		}
		return events.Publish(ctx, r.pool, agentEvent(events.AgentCreated, *created, nil))
	})
}

// writeAgentEnvironments replaces an agent's environment links.
func writeAgentEnvironments(ctx context.Context, tx pgx.Tx, agentTypeID string, envs []AgentEnvironment) error {
	if _, err := tx.Exec(ctx, `DELETE FROM agent_environments WHERE agent_type_id=$1`, agentTypeID); err != nil {
		return fmt.Errorf("clear agent environments: %w", err)
	}
	for _, e := range envs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent_environments (agent_type_id, environment_id, is_default) VALUES ($1,$2,$3)`,
			agentTypeID, e.EnvironmentID, e.IsDefault,
		); err != nil {
			return fmt.Errorf("attach environment %q: %w", e.EnvironmentID, err)
		}
	}
	return nil
}

func (r *PostgresRepository) GetAgentType(ctx context.Context, id string) (*AgentType, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+agentTypeColumns+`
		FROM agent_types WHERE id = $1 AND deleted_at IS NULL`, id)
	return scanAgentType(row)
}

// GetAgentTypeWithDeleted returns the agent type even when it is deleted. Run reads use it to show a deleted agent's runs.
func (r *PostgresRepository) GetAgentTypeWithDeleted(ctx context.Context, id string) (*AgentType, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+agentTypeColumns+`
		FROM agent_types WHERE id = $1`, id)
	return scanAgentType(row)
}

// lockAgentType reads a live agent type and locks its row until the transaction ends.
func (r *PostgresRepository) lockAgentType(ctx context.Context, id string) (*AgentType, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+agentTypeColumns+`
		FROM agent_types WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id)
	return scanAgentType(row)
}

func (r *PostgresRepository) GetAgentTypeBySlug(ctx context.Context, tenantID, slug string) (*AgentType, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+agentTypeColumns+`
		FROM agent_types WHERE tenant_id = $1 AND slug = $2 AND deleted_at IS NULL`, tenantID, slug)
	return scanAgentType(row)
}

// ListDeclaredAgentTypes returns the slug and resource UID of a tenant's live agent types projected from resources.
func (r *PostgresRepository) ListDeclaredAgentTypes(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	return r.listDeclared(ctx, `SELECT slug, resource_uid FROM agent_types WHERE tenant_id = $1 AND source = 'crd' AND deleted_at IS NULL ORDER BY slug`, tenantID)
}

func (r *PostgresRepository) ListAgentTypes(ctx context.Context, tenantID string) ([]AgentType, error) {
	return r.listAgentTypes(ctx, `
		SELECT `+agentTypeColumns+`
		FROM agent_types WHERE tenant_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`, tenantID)
}

// ListAgentTypesWithDeleted lists a tenant's agent types, deleted ones included. Run reads scope by it.
func (r *PostgresRepository) ListAgentTypesWithDeleted(ctx context.Context, tenantID string) ([]AgentType, error) {
	return r.listAgentTypes(ctx, `
		SELECT `+agentTypeColumns+`
		FROM agent_types WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
}

func (r *PostgresRepository) listAgentTypes(ctx context.Context, query string, args ...any) ([]AgentType, error) {
	rows, err := r.pool.Query(ctx, query, args...)
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
	// identity_mode is deliberately absent from the SET list: it is immutable
	// after creation (the service layer rejects changes; the storage layer never
	// writes it on update).
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, tx pgx.Tx) error {
		before, err := r.lockAgentType(ctx, at.ID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE agent_types
			SET name=$1, slug=$2, description=$3, system_prompt=$4, framework=$5, grant_scopes=$6, max_concurrent_runs_per_pod=$7, model_id=$8, updated_at=$9, connectivity=$10::jsonb, connectors=$11::jsonb, mcp_tools=$12, guardrails=$13::jsonb, resource_generation=$15, repository=NULLIF($16, '')
			WHERE id=$14 AND deleted_at IS NULL`,
			at.Name, at.Slug, at.Description, at.SystemPrompt, string(at.Framework), at.GrantScopes, at.MaxConcurrentRunsPerPod, at.ModelID, time.Now().UTC(), string(connectivityJSON), string(connectorsJSON), at.MCPTools,
			guardrailsColumn(at.Guardrails), at.ID, at.ResourceGeneration, at.Repository,
		)
		if err != nil {
			return fmt.Errorf("update agent_type: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err = writeAgentEnvironments(ctx, tx, at.ID, at.Environments); err != nil {
			return err
		}
		after, err := r.GetAgentType(ctx, at.ID)
		if err != nil {
			return err
		}
		changed := events.Changed(newAgentFields(*before), newAgentFields(*after))
		if len(changed) == 0 {
			return nil
		}
		return events.Publish(ctx, r.pool, agentEvent(events.AgentUpdated, *after, changed))
	})
}

func (r *PostgresRepository) SetAgentTypeState(ctx context.Context, id string, from, to AgentTypeState) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE agent_types SET state=$1, updated_at=$2,
			activated_at = CASE WHEN $1 = 'active' THEN $2 ELSE activated_at END
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

// ListActiveAgentTypes returns the active agent types of every tenant.
func (r *PostgresRepository) ListActiveAgentTypes(ctx context.Context) ([]AgentType, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+agentTypeColumns+`
		FROM agent_types WHERE state = $1 ORDER BY created_at`, string(AgentTypeStateActive))
	if err != nil {
		return nil, fmt.Errorf("query active agent_types: %w", err)
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

func (r *PostgresRepository) ListAllAgentTypeSlugs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT slug FROM agent_types WHERE deleted_at IS NULL ORDER BY slug`)
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

// DeleteAgentType removes the row. Only a failed first provisioning uses it; agent deletion is SoftDeleteAgentType.
func (r *PostgresRepository) DeleteAgentType(ctx context.Context, id string) error {
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		at, err := r.GetAgentTypeWithDeleted(ctx, id)
		if err != nil {
			return err
		}
		if _, err := r.pool.Exec(ctx, `DELETE FROM agent_types WHERE id=$1`, id); err != nil {
			return fmt.Errorf("delete agent_type: %w", err)
		}
		return events.Publish(ctx, r.pool, agentEvent(events.AgentDeleted, *at, nil))
	})
}

// SoftDeleteAgentType detaches the agent's environments and marks it deleted. The row stays for its runs.
func (r *PostgresRepository) SoftDeleteAgentType(ctx context.Context, id string) error {
	// The row lock comes first. A concurrent UpdateAgentType waits and then matches no row,
	// so it cannot re-attach environments after they are removed.
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, tx pgx.Tx) error {
		before, err := r.lockAgentType(ctx, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `
			UPDATE agent_types SET state=$1, deleted_at=$2, updated_at=$2
			WHERE id=$3`,
			string(AgentTypeStateDeleted), now, id,
		); err != nil {
			return fmt.Errorf("soft delete agent_type: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM agent_environments WHERE agent_type_id=$1`, id); err != nil {
			return fmt.Errorf("delete agent environments: %w", err)
		}
		return events.Publish(ctx, r.pool, agentEvent(events.AgentDeleted, *before, nil))
	})
}

// ── Runs ──────────────────────────────────────────────────────────────────────

func (r *PostgresRepository) CreateRun(ctx context.Context, run Run) error {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	// Trigger attribution is written at insert and never updated: it records why
	// the run exists, which cannot change after it starts.
	var triggerID, triggerName, deliveryID *string
	triggerIDs := append([]string{}, run.TriggerIDs...)
	sourcesJSON, encErr := json.Marshal(mergeRunSources(nil, run.Sources))
	if encErr != nil {
		return fmt.Errorf("encode run sources: %w", encErr)
	}
	if run.StartedBy != nil {
		triggerID = storage.NullIfZero(run.StartedBy.TriggerID)
		triggerName = storage.NullIfZero(run.StartedBy.TriggerName)
		deliveryID = storage.NullIfZero(run.StartedBy.DeliveryID)
		if id := run.StartedBy.TriggerID; id != "" && !slices.Contains(triggerIDs, id) {
			triggerIDs = append(triggerIDs, id)
		}
	}
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		_, err := r.pool.Exec(ctx, `
			INSERT INTO runs
				(id, tenant_id, agent_type_id, state, pod_ordinal, created_by, created_at, updated_at,
				 trigger_id, trigger_name, delivery_id, initial_message, trace_parent, caused_by, chain_depth, trigger_ids, sources)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
			run.ID, run.TenantID, run.AgentTypeID, string(RunStatePending), -1, run.CreatedBy, now, now,
			triggerID, triggerName, deliveryID, run.InitialMessage, storage.NullIfZero(run.TraceParent),
			storage.NullIfZero(run.CausedBy), run.ChainDepth, triggerIDs, sourcesJSON,
		)
		if err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		created, err := r.GetRun(ctx, run.ID)
		if err != nil {
			return err
		}
		return events.Publish(ctx, r.pool, runEvent(events.RunCreated, *created, newRunEntity(*created)))
	})
}

// selectRun joins agent_types so every run read carries the agent's identity mode, slug and name.
// Agent deletion keeps the agent_types row, so the inner join never drops a run.
const selectRun = `SELECT ` + runColumns + ` FROM runs r JOIN agent_types at ON at.id = r.agent_type_id`

const runColumns = `
	r.id, r.tenant_id, r.agent_type_id, r.state, r.pod_ordinal, r.created_by, r.started_at, r.completed_at, r.created_at, r.updated_at, r.summary, r.delegation_jti, at.identity_mode, at.slug, at.name,
	r.trigger_id, r.trigger_name, r.delivery_id, r.environment_id, r.trace_parent, r.deleted_at, r.caused_by, r.chain_depth, r.trigger_ids, r.sources`

// GetRun returns the run, a terminated one included.
func (r *PostgresRepository) GetRun(ctx context.Context, id string) (*Run, error) {
	row := r.pool.QueryRow(ctx, selectRun+` WHERE r.id=$1`, id)
	return scanRun(row)
}

// ListRuns returns up to q.Limit runs in scope, newest first, after q.After.
// The scope is part of the query, so a page holds only runs the caller may read.
func (r *PostgresRepository) ListRuns(ctx context.Context, tenantID string, q RunListQuery) ([]Run, error) {
	query, args := appendScope(selectRun+` WHERE r.tenant_id=$1 AND r.deleted_at IS NULL`, []any{tenantID}, q.Scope)
	if len(q.States) > 0 {
		args = append(args, stateStrings(q.States))
		query += fmt.Sprintf(" AND r.state = ANY($%d)", len(args))
	}
	if q.StartedByTrigger {
		// Matches scanRun, which reads a run as trigger-started only when trigger_id is non-empty.
		query += " AND r.trigger_id <> ''"
	}
	if !q.CreatedAfter.IsZero() {
		args = append(args, q.CreatedAfter)
		query += fmt.Sprintf(" AND r.created_at >= $%d", len(args))
	}
	if !q.CreatedBefore.IsZero() {
		args = append(args, q.CreatedBefore)
		query += fmt.Sprintf(" AND r.created_at < $%d", len(args))
	}
	if q.After != nil {
		args = append(args, q.After.CreatedAt, q.After.ID)
		query += fmt.Sprintf(" AND (r.created_at, r.id) < ($%d, $%d)", len(args)-1, len(args))
	}
	// id breaks created_at ties, which keeps both the page boundary and the cursor stable.
	args = append(args, q.Limit)
	query += fmt.Sprintf(" ORDER BY r.created_at DESC, r.id DESC LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, query, args...)
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

// RunUsageTotals sums inference_usage per run for the given runs in one query,
// using idx_inference_usage_run_id. The table belongs to the inference gateway;
// a cross-service read at query time is allowed by the database migrations ADR.
func (r *PostgresRepository) RunUsageTotals(ctx context.Context, tenantID string, runIDs []string) (map[string]RunUsage, error) {
	out := make(map[string]RunUsage, len(runIDs))
	if len(runIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT run_id,
		       COALESCE(SUM(estimated_cost_usd), 0)::float8,
		       COALESCE(SUM(prompt_tokens), 0)::bigint,
		       COALESCE(SUM(completion_tokens), 0)::bigint,
		       COUNT(*)
		FROM inference_usage
		WHERE tenant_id = $1 AND run_id = ANY($2)
		GROUP BY run_id`, tenantID, runIDs)
	if err != nil {
		return nil, fmt.Errorf("query run usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var u RunUsage
		if err := rows.Scan(&id, &u.CostUSD, &u.InputTokens, &u.OutputTokens, &u.RequestCount); err != nil {
			return nil, fmt.Errorf("scan run usage: %w", err)
		}
		out[id] = u
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SetRunState(ctx context.Context, id string, from, to RunState) error {
	now := time.Now().UTC()
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		var tag interface{ RowsAffected() int64 }
		var err error
		if to == RunStateCompleted || to == RunStateFailed {
			tag, err = r.pool.Exec(ctx, `
				UPDATE runs SET state=$1, completed_at=$2, updated_at=$2
				WHERE id=$3 AND state=$4 AND deleted_at IS NULL AND state IN (`+podOccupancySQLList()+`)`,
				string(to), now, id, string(from),
			)
		} else {
			tag, err = r.pool.Exec(ctx, `
				UPDATE runs SET state=$1, updated_at=$2
				WHERE id=$3 AND state=$4 AND deleted_at IS NULL AND state IN (`+podOccupancySQLList()+`)`,
				string(to), now, id, string(from),
			)
		}
		if err != nil {
			return fmt.Errorf("set run state: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		return r.publishState(ctx, id, from, to)
	})
}

// publishState records the event of run id moving from from to to, if that change has one.
func (r *PostgresRepository) publishState(ctx context.Context, id string, from, to RunState) error {
	if _, ok := stateEventType(from, to); !ok {
		return nil
	}
	run, err := r.GetRun(ctx, id)
	if err != nil {
		return err
	}
	lastError, err := r.lastRunError(ctx, *run)
	if err != nil {
		return err
	}
	ev, _ := stateEvent(from, *run, lastError)
	return events.Publish(ctx, r.pool, ev)
}

// failureCodeKey carries the run_error code of a failure recorded after its transition.
type failureCodeKey struct{}

// withFailureCode makes a failed transition in ctx carry code.
func withFailureCode(ctx context.Context, code string) context.Context {
	return context.WithValue(ctx, failureCodeKey{}, code)
}

// failureCode is the run_error code withFailureCode put in ctx.
func failureCode(ctx context.Context) (string, bool) {
	code, ok := ctx.Value(failureCodeKey{}).(string)
	return code, ok
}

// lastRunError is the code of a failed run's latest run_error, or "" for any
// other run. A code in ctx stands for a run_error not yet written.
func (r *PostgresRepository) lastRunError(ctx context.Context, run Run) (string, error) {
	if run.State != RunStateFailed {
		return "", nil
	}
	if code, ok := failureCode(ctx); ok {
		return code, nil
	}
	var code string
	err := r.pool.QueryRow(ctx, `
		SELECT coalesce(payload->>'code', '') FROM run_events
		WHERE  run_id = $1 AND event_type = 'run_error'
		ORDER  BY sequence_no DESC LIMIT 1`, run.ID).Scan(&code)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("read last run error: %w", err)
	}
	return code, nil
}

// AssignPod moves a claimed run to starting on podOrdinal. The claim token stops a
// worker whose claim went stale from assigning a run that another worker now holds.
func (r *PostgresRepository) AssignPod(ctx context.Context, runID string, claimedAt time.Time, podOrdinal int) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE runs SET state=$1, pod_ordinal=$2, started_at=$3, updated_at=$3
		WHERE id=$4 AND state=$5 AND claimed_at=$6`,
		string(RunStateStarting), podOrdinal, now, runID, string(RunStatePending), claimToken(claimedAt),
	)
	if err != nil {
		return fmt.Errorf("assign pod: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// ClaimPendingRuns claims up to limit due pending runs, oldest first, with SKIP LOCKED.
// The claim time now is the token that AssignPod and ReleaseRunClaim check.
func (r *PostgresRepository) ClaimPendingRuns(ctx context.Context, limit int, now time.Time) ([]Run, error) {
	rows, err := r.pool.Query(ctx, `
		WITH claimed AS (
		    UPDATE runs SET claimed_at = $2, updated_at = $2
		    WHERE id IN (
		        SELECT id FROM runs
		        WHERE  state = 'pending' AND claimed_at IS NULL AND not_before <= $2 AND deleted_at IS NULL
		        ORDER  BY created_at
		        LIMIT  $1
		        FOR UPDATE SKIP LOCKED)
		    RETURNING id, initial_message)
		SELECT `+runColumns+`, c.initial_message
		FROM runs r JOIN agent_types at ON at.id = r.agent_type_id JOIN claimed c ON c.id = r.id
		ORDER BY r.created_at`,
		limit, claimToken(now))
	if err != nil {
		return nil, fmt.Errorf("claim pending runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var msg string
		run, err := scanRun(rows, &msg)
		if err != nil {
			return nil, err
		}
		run.InitialMessage = msg
		out = append(out, *run)
	}
	return out, rows.Err()
}

// QueueRun makes a pending run claimable from notBefore. Returns ErrConflict when
// the run is gone or has left pending.
func (r *PostgresRepository) QueueRun(ctx context.Context, runID string, notBefore time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE runs SET not_before = $2 WHERE id = $1 AND state = 'pending'`, runID, notBefore)
	if err != nil {
		return fmt.Errorf("queue run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// ReleaseRunClaim returns a run claimed at claimedAt to the queue until notBefore.
// Returns ErrConflict when the run is gone, has moved on, or another worker holds it.
func (r *PostgresRepository) ReleaseRunClaim(ctx context.Context, id string, claimedAt, notBefore time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE runs SET state = 'pending', pod_ordinal = -1, started_at = NULL,
		       claimed_at = NULL, not_before = $3, updated_at = $4
		WHERE  id = $1 AND claimed_at = $2 AND state IN ('pending', 'starting')`,
		id, claimToken(claimedAt), notBefore, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("release run claim: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// ReclaimStaleRunClaims releases pending runs claimed before cutoff by a worker
// that never came back, and returns their IDs. A run a dead worker left in
// starting stays for the stale-run reaper.
func (r *PostgresRepository) ReclaimStaleRunClaims(ctx context.Context, cutoff time.Time) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE runs SET claimed_at = NULL, updated_at = $2
		WHERE  state = 'pending' AND claimed_at < $1
		RETURNING id`, cutoff, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("reclaim stale run claims: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// claimToken rounds t to the microsecond precision Postgres stores, so a claim
// time read back compares equal to the one written.
func claimToken(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }

// MarkRunFailed transitions a run to the failed terminal state and stamps
// completed_at. Unlike SetRunState it is not a compare-and-swap — it cleans
// up a run whose start failed from whatever non-terminal state it reached. Zero
// rows affected (run gone or already ended) is not an error.
func (r *PostgresRepository) MarkRunFailed(ctx context.Context, id string) error {
	now := time.Now().UTC()
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		tag, err := r.pool.Exec(ctx, `
			UPDATE runs SET state=$1, completed_at=$2, updated_at=$2
			WHERE id=$3 AND state IN (`+podOccupancySQLList()+`)`,
			string(RunStateFailed), now, id,
		)
		if err != nil {
			return fmt.Errorf("mark run failed: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		return r.publishState(ctx, id, "", RunStateFailed)
	})
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

// SetRunDelegationJTI stamps the delegation issuance minted for a delegated
// run at start (delegated-agent-identity ADR, Decision 3). Zero rows affected
// (run gone) is not an error — the deferred start-failure cleanup may have
// already removed the row, and revocation is keyed on the JTI the caller holds.
func (r *PostgresRepository) SetRunDelegationJTI(ctx context.Context, runID, jti string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE runs SET delegation_jti=$1, updated_at=$2 WHERE id=$3`,
		jti, time.Now().UTC(), runID,
	)
	if err != nil {
		return fmt.Errorf("set run delegation jti: %w", err)
	}
	return nil
}

// TerminateRun ends a live run as terminated and marks the run deleted. A run that already ended
// keeps its state. The row and its run_events stay. Returns ErrNotFound for a run already terminated.
func (r *PostgresRepository) TerminateRun(ctx context.Context, id string) (*Run, error) {
	var run *Run
	err := storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		live := `state IN (` + podOccupancySQLList() + `)`
		tag, err := r.pool.Exec(ctx, `
			UPDATE runs SET state = CASE WHEN `+live+` THEN $2 ELSE state END,
			       completed_at = CASE WHEN `+live+` THEN $3 ELSE completed_at END,
			       deleted_at = $3, updated_at = $3
			WHERE id = $1 AND deleted_at IS NULL`,
			id, string(RunStateTerminated), time.Now().UTC())
		if err != nil {
			return fmt.Errorf("terminate run: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if run, err = r.GetRun(ctx, id); err != nil {
			return err
		}
		changed := []string{"deleted_at"}
		if run.State == RunStateTerminated {
			changed = []string{"state", "completed_at", "deleted_at"}
		}
		return events.Publish(ctx, r.pool, runEvent(events.RunTerminated, *run, newRunEntity(*run), changed...))
	})
	return run, err
}

// CancelRunsByAgentType ends every unfinished run of an agent type. A run that finished its
// turn (idle or completing) completes and any other run fails.
func (r *PostgresRepository) CancelRunsByAgentType(ctx context.Context, agentTypeID string) ([]Run, error) {
	var cancelled []Run
	err := storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		var err error
		cancelled, err = r.endRuns(ctx, `
			UPDATE runs
			SET state = CASE WHEN state IN ('idle','completing') THEN 'completed' ELSE 'failed' END,
			    completed_at = $2, updated_at = $2
			WHERE agent_type_id = $1 AND state IN ('pending','starting','active','idle','completing')`,
			agentTypeID, time.Now().UTC())
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("cancel runs for agent type: %w", err)
	}
	return cancelled, nil
}

// endRuns runs a bulk update that moves runs to a terminal state and records the state event of each.
// It returns the ended runs, so each event takes its tenant and agent from its own row.
func (r *PostgresRepository) endRuns(ctx context.Context, update string, args ...any) ([]Run, error) {
	rows, err := r.pool.Query(ctx, `
		WITH ended AS (`+update+` RETURNING *)
		SELECT `+runColumns+` FROM ended r JOIN agent_types at ON at.id = r.agent_type_id`, args...)
	if err != nil {
		return nil, err
	}
	ended, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Run, error) {
		run, scanErr := scanRun(row)
		if scanErr != nil {
			return Run{}, scanErr
		}
		return *run, nil
	})
	if err != nil {
		return nil, err
	}
	// The rows are read before the events are written, which share the transaction's connection.
	for _, run := range ended {
		lastError, err := r.lastRunError(ctx, run)
		if err != nil {
			return nil, err
		}
		if ev, ok := stateEvent("", run, lastError); ok {
			if err := events.Publish(ctx, r.pool, ev); err != nil {
				return nil, err
			}
		}
	}
	return ended, nil
}

// podOccupancySQLList renders the pod-occupying run states as a SQL literal
// list ('pending','starting',...). The states are inlined rather than bound as
// a query parameter so the predicate stays textually identical to the partial
// index idx_runs_active, which the planner needs to match it.
func podOccupancySQLList() string {
	states := PodOccupyingRunStates()
	quoted := make([]string, len(states))
	for i, s := range states {
		quoted[i] = "'" + string(s) + "'"
	}
	return strings.Join(quoted, ", ")
}

// activeRunsPerPodQuery counts the runs occupying each pod of an agent type.
// Derived from RunState.OccupiesPod so the SQL can never drift from the states
// the Go code treats as resident on a pod.
//
// Built at init rather than written as a constant on purpose. Spelling the
// states out here would restore the hand-maintained list this change exists to
// remove, and it would be the copy nothing checks: the predicate has to match
// both OccupiesPod and the idx_runs_active partial index, and only generating
// it keeps all three in step. Do not fold it back into a literal.
var activeRunsPerPodQuery = fmt.Sprintf(`
		SELECT pod_ordinal, COUNT(*) FROM runs
		WHERE agent_type_id=$1 AND state IN (%s)
		GROUP BY pod_ordinal`, podOccupancySQLList())

func (r *PostgresRepository) ActiveRunsPerPod(ctx context.Context, agentTypeID string) (map[int]int, error) {
	rows, err := r.pool.Query(ctx, activeRunsPerPodQuery, agentTypeID)
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
// reaped runs in their new terminal state, each with its event recorded.
//
//   - active / starting → failed (session-manager died mid-turn)
//   - pending → failed (never queued, or no scheduler took it)
//   - idle → completed  (session-manager died after a clean turn; idle timeout
//     never fired because the pod restarted)
func (r *PostgresRepository) ReapStaleRuns(ctx context.Context, staleDuration time.Duration) ([]Run, error) {
	now := time.Now().UTC()
	var reaped []Run
	err := storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		var err error
		reaped, err = r.endRuns(ctx, `
			UPDATE runs
			SET state = CASE
				WHEN state IN ('active','starting','pending') THEN 'failed'
				ELSE 'completed'
			END,
			completed_at = $1,
			updated_at   = $1
			WHERE state IN ('active','starting','pending','idle')
			  AND updated_at < $2
			  AND NOT EXISTS (
			      SELECT 1 FROM run_events e
			      WHERE e.run_id = runs.id AND e.created_at >= $2
			  )`,
			now, now.Add(-staleDuration))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("reap stale runs: %w", err)
	}
	return reaped, nil
}

// ── Events ────────────────────────────────────────────────────────────────────

// AppendRunEvent records an event, and the spine event it stands for in the same transaction.
// A caller that also publishes the event sets CreatedAt, so the live stream and a later replay carry the same time.
func (r *PostgresRepository) AppendRunEvent(ctx context.Context, e RunEvent) (int64, error) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = eventTime()
	}
	if !recordsActivity[e.EventType] {
		return r.insertRunEvent(ctx, e)
	}
	var seqNo int64
	err := storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		var err error
		if seqNo, err = r.insertRunEvent(ctx, e); err != nil {
			return err
		}
		run, err := r.GetRun(ctx, e.RunID)
		if err != nil {
			return err
		}
		if ev, ok := activityEvent(*run, e); ok {
			return events.Publish(ctx, r.pool, ev)
		}
		return nil
	})
	return seqNo, err
}

func (r *PostgresRepository) insertRunEvent(ctx context.Context, e RunEvent) (int64, error) {
	var seqNo int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO run_events (run_id, event_type, payload, created_at)
		VALUES ($1,$2,$3,$4)
		RETURNING sequence_no`,
		e.RunID, e.EventType, e.Payload, e.CreatedAt,
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

// AppendRunMessage records a message and its agents.run.message_received event. A
// message with a chain adds its triggers to the run and keeps the cause with the
// highest chain depth.
func (r *PostgresRepository) AppendRunMessage(ctx context.Context, m RunMessage) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
		_, err := r.pool.Exec(ctx, `
			INSERT INTO run_messages (id, run_id, content, created_at)
			VALUES ($1,$2,$3,$4)`,
			m.ID, m.RunID, m.Content, time.Now().UTC(),
		)
		if err != nil {
			return fmt.Errorf("insert run_message: %w", err)
		}
		triggers := append([]string{}, m.Triggers...)
		if m.From != nil {
			triggers = append(triggers, m.From.TriggerID)
		}
		if m.From != nil || len(triggers) > 0 || m.ChainDepth > 0 {
			// The union keeps the run's triggers first, in the order they joined.
			_, err = r.pool.Exec(ctx, `
				UPDATE runs SET
				    trigger_ids = ARRAY(
				        SELECT t FROM unnest(trigger_ids || $2::text[]) WITH ORDINALITY AS u(t, n)
				        WHERE t <> '' GROUP BY t ORDER BY min(n)),
				    caused_by   = CASE WHEN $3::uuid IS NOT NULL AND $4 >= chain_depth THEN $3::uuid ELSE caused_by END,
				    chain_depth = GREATEST(chain_depth, $4)
				WHERE id = $1`,
				m.RunID, triggers, storage.NullIfZero(m.CausedBy), m.ChainDepth)
			if err != nil {
				return fmt.Errorf("record message trigger: %w", err)
			}
		}
		run, err := r.GetRun(ctx, m.RunID)
		if err != nil {
			return err
		}
		return events.Publish(ctx, r.pool, messageEvent(*run, m.From))
	})
}

// RecordSandboxDeleted records agents.sandbox.deleted.v1 for run's sandbox once.
func (r *PostgresRepository) RecordSandboxDeleted(ctx context.Context, run Run) error {
	return events.PublishOnce(ctx, r.pool, run.ID+"|"+sandboxfolder.SandboxPodName(run.ID), sandboxDeletedEvent(run))
}

// ── helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(...any) error
}

// agentTypeColumns is the projection scanAgentType expects, in its order.
// Every agent_type read selects it rather than repeating the list: pgx matches
// columns to scan destinations positionally and errors on a count mismatch, so
// a query that drifts from the scanner fails on every row at runtime rather
// than at compile time.
// The last column aggregates the agent's environment links, default first.
const agentTypeColumns = `id, tenant_id, name, slug, description, system_prompt, framework, grant_scopes, max_concurrent_runs_per_pod, state, model_id, created_at, updated_at, connectivity, connectors, mcp_tools, identity_mode, guardrails, created_by, activated_at, source, COALESCE(resource_uid, ''), resource_generation, COALESCE(repository, ''),
	COALESCE((SELECT jsonb_agg(jsonb_build_object('environment_id', ae.environment_id, 'is_default', ae.is_default) ORDER BY ae.is_default DESC, ae.environment_id)
		FROM agent_environments ae WHERE ae.agent_type_id = agent_types.id), '[]'::jsonb)`

func scanAgentType(s scanner) (*AgentType, error) {
	var at AgentType
	var state, framework, identityMode string
	var connectivityJSON, connectorsJSON, guardrailsJSON, environmentsJSON []byte
	var activatedAt *time.Time
	err := s.Scan(
		&at.ID, &at.TenantID, &at.Name, &at.Slug, &at.Description, &at.SystemPrompt, &framework,
		&at.GrantScopes, &at.MaxConcurrentRunsPerPod, &state, &at.ModelID, &at.CreatedAt, &at.UpdatedAt, &connectivityJSON, &connectorsJSON, &at.MCPTools, &identityMode, &guardrailsJSON, &at.CreatedBy, &activatedAt, &at.Source, &at.ResourceUID, &at.ResourceGeneration, &at.Repository, &environmentsJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan agent_type: %w", err)
	}
	at.State = AgentTypeState(state)
	if activatedAt != nil {
		at.ActivatedAt = *activatedAt
	}
	at.Framework = AgentFramework(framework)
	at.IdentityMode = IdentityMode(identityMode)
	if len(connectivityJSON) > 0 {
		if uErr := json.Unmarshal(connectivityJSON, &at.Connectivity); uErr != nil {
			return nil, fmt.Errorf("unmarshal connectivity: %w", uErr)
		}
	}
	if len(guardrailsJSON) > 0 {
		if uErr := json.Unmarshal(guardrailsJSON, &at.Guardrails); uErr != nil {
			return nil, fmt.Errorf("unmarshal guardrails: %w", uErr)
		}
	}
	if len(connectorsJSON) > 0 {
		if uErr := json.Unmarshal(connectorsJSON, &at.Connectors); uErr != nil {
			return nil, fmt.Errorf("unmarshal connectors: %w", uErr)
		}
	}
	if uErr := json.Unmarshal(environmentsJSON, &at.Environments); uErr != nil {
		return nil, fmt.Errorf("unmarshal environments: %w", uErr)
	}
	return &at, nil
}

// scanRun scans the runColumns, then extra into the given destinations.
func scanRun(s scanner, extra ...any) (*Run, error) {
	var r Run
	var state, identityMode string
	var triggerID, triggerName, deliveryID, traceParent, causedBy *string
	var sourcesJSON []byte
	err := s.Scan(append([]any{
		&r.ID, &r.TenantID, &r.AgentTypeID, &state, &r.PodOrdinal,
		&r.CreatedBy, &r.StartedAt, &r.CompletedAt, &r.CreatedAt, &r.UpdatedAt,
		&r.Summary, &r.DelegationJTI, &identityMode, &r.AgentSlug, &r.AgentName,
		&triggerID, &triggerName, &deliveryID, &r.EnvironmentID, &traceParent,
		&r.DeletedAt, &causedBy, &r.ChainDepth, &r.TriggerIDs, &sourcesJSON,
	}, extra...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan run: %w", err)
	}
	r.Sources = []RunSource{}
	if len(sourcesJSON) > 0 {
		if err := json.Unmarshal(sourcesJSON, &r.Sources); err != nil {
			return nil, fmt.Errorf("decode run sources: %w", err)
		}
	}
	r.State = RunState(state)
	r.IdentityMode = IdentityMode(identityMode).OrDefault()
	r.TraceParent = storage.Deref(traceParent)
	r.CausedBy = storage.Deref(causedBy)
	r.TraceID = observability.SampledTraceID(r.TraceParent)
	// The trigger id is what makes the attribution real; a name or delivery
	// without one would be a run claiming automation without naming what
	// automated it, so the whole block hangs off the id being present.
	if triggerID != nil && *triggerID != "" {
		r.StartedBy = &StartedByTrigger{
			TriggerID:   *triggerID,
			TriggerName: storage.Deref(triggerName),
			DeliveryID:  storage.Deref(deliveryID),
		}
	}
	return &r, nil
}

// AppendRunSources adds sources to the run in one statement, skipping any whose
// URL the run already has.
func (r *PostgresRepository) AppendRunSources(ctx context.Context, runID string, sources []RunSource) error {
	add := mergeRunSources(nil, sources)
	if len(add) == 0 {
		return nil
	}
	addJSON, err := json.Marshal(add)
	if err != nil {
		return fmt.Errorf("encode run sources: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE runs SET
			sources = sources || COALESCE((
				SELECT jsonb_agg(n.e ORDER BY n.i)
				FROM jsonb_array_elements($1::jsonb) WITH ORDINALITY AS n(e, i)
				WHERE NOT EXISTS (
					SELECT 1 FROM jsonb_array_elements(runs.sources) o WHERE o->>'url' = n.e->>'url')
			), '[]'::jsonb),
			updated_at = $2
		WHERE id = $3`, addJSON, time.Now().UTC(), runID)
	if err != nil {
		return fmt.Errorf("append run sources: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetRunEnvironment records the environment of the run's current sandbox. Nil is the built-in default.
func (r *PostgresRepository) SetRunEnvironment(ctx context.Context, runID string, environmentID *string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE runs SET environment_id=$1, updated_at=$2 WHERE id=$3`, environmentID, time.Now().UTC(), runID)
	if err != nil {
		return fmt.Errorf("set run environment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func isForeignKeyViolation(err error) bool { return hasSQLState(err, "23503") }

func isUniqueViolation(err error) bool { return hasSQLState(err, "23505") }

// hasSQLState reports whether err wraps a Postgres error with the given SQLSTATE code.
func hasSQLState(err error, code string) bool {
	var pe interface{ SQLState() string }
	return errors.As(err, &pe) && pe.SQLState() == code
}

// CountRunOutcomes counts completed and failed runs finished in [from, to), using
// idx_runs_tenant_completed.
func (r *PostgresRepository) CountRunOutcomes(ctx context.Context, tenantID string, scope RunScope, from, to time.Time) (RunOutcomes, error) {
	query, args := appendScope(`
		SELECT COUNT(*) FILTER (WHERE r.state = 'completed'),
		       COUNT(*) FILTER (WHERE r.state = 'failed')
		FROM runs r JOIN agent_types at ON at.id = r.agent_type_id
		WHERE r.tenant_id = $1 AND r.deleted_at IS NULL
		  AND r.completed_at >= $2 AND r.completed_at < $3
		  AND r.state IN ('completed', 'failed')`,
		[]any{tenantID, from, to}, scope)
	var out RunOutcomes
	err := r.pool.QueryRow(ctx, query, args...).Scan(&out.Completed, &out.Failed)
	if err != nil {
		return RunOutcomes{}, fmt.Errorf("count run outcomes: %w", err)
	}
	return out, nil
}

// finishedRunsSQL selects the runs finished in [$2, $3) with their duration in ms.
// A run that never started counts from its creation. Callers append the scope.
const finishedRunsSQL = `
	SELECT r.state, GREATEST(EXTRACT(EPOCH FROM (r.completed_at - COALESCE(r.started_at, r.created_at))) * 1000, 0) AS ms
	FROM runs r JOIN agent_types at ON at.id = r.agent_type_id
	WHERE r.tenant_id = $1 AND r.deleted_at IS NULL
	  AND r.completed_at >= $2 AND r.completed_at < $3
	  AND r.state IN ('completed', 'failed')`

// RunStats counts the runs in scope created in [from, to) and those a trigger
// started, then the runs finished in it by outcome, duration percentile and
// histogram bucket, then every run in scope by its current state.
func (r *PostgresRepository) RunStats(ctx context.Context, tenantID string, scope RunScope, from, to time.Time) (RunStats, error) {
	var out RunStats
	query, args := appendScope(`
		SELECT COUNT(*), COUNT(r.trigger_id)
		FROM runs r JOIN agent_types at ON at.id = r.agent_type_id
		WHERE r.tenant_id = $1 AND r.deleted_at IS NULL AND r.created_at >= $2 AND r.created_at < $3`,
		[]any{tenantID, from, to}, scope)
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&out.Total, &out.Triggered); err != nil {
		return RunStats{}, fmt.Errorf("count runs: %w", err)
	}

	current, err := r.runStateCounts(ctx, tenantID, scope)
	if err != nil {
		return RunStats{}, err
	}
	out.Current = current

	inner, args := appendScope(finishedRunsSQL, []any{tenantID, from, to}, scope)
	var median, p95, p99, longest float64
	err = r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE state = 'completed'), COUNT(*) FILTER (WHERE state = 'failed'),
		       COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY ms), 0),
		       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY ms), 0),
		       COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY ms), 0), COALESCE(MAX(ms), 0)
		FROM (`+inner+`) f`, args...).Scan(&out.Completed, &out.Failed, &median, &p95, &p99, &longest)
	if err != nil {
		return RunStats{}, fmt.Errorf("finished run durations: %w", err)
	}
	// p99 and the longest run round up so no run falls above its own histogram edge.
	d := &out.Durations
	d.MedianMs, d.P95Ms, d.P99Ms, d.LongestMs =
		int64(math.Round(median)), int64(math.Round(p95)), int64(math.Ceil(p99)), int64(math.Ceil(longest))
	d.Buckets = make([]int64, histogramBuckets)
	finished := out.Completed + out.Failed
	if finished == 0 {
		return out, nil
	}
	var rangeMs int64
	rangeMs, d.Capped = histogramRange(finished, d.P99Ms, d.LongestMs)

	args = append(args, rangeMs)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT CASE WHEN ms > $%[1]d THEN %[2]d ELSE LEAST(width_bucket(ms::numeric, 0, $%[1]d::numeric, %[3]d), %[3]d) END AS bucket, COUNT(*)
		FROM (`+inner+`) f GROUP BY bucket`, len(args), histogramBuckets+1, histogramBuckets), args...)
	if err != nil {
		return RunStats{}, fmt.Errorf("finished run buckets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket int
		var n int64
		if err := rows.Scan(&bucket, &n); err != nil {
			return RunStats{}, fmt.Errorf("scan finished run buckets: %w", err)
		}
		if bucket > histogramBuckets {
			d.Overflow = n
		} else {
			d.Buckets[bucket-1] = n
		}
	}
	if err := rows.Err(); err != nil {
		return RunStats{}, fmt.Errorf("finished run buckets: %w", err)
	}
	return out, nil
}

// runStateCounts counts every run in scope by agent type and current state, whatever its age.
func (r *PostgresRepository) runStateCounts(ctx context.Context, tenantID string, scope RunScope) (RunStateCounts, error) {
	out := RunStateCounts{ByState: map[RunState]int64{}, LiveByAgentType: map[string]int64{}}
	query, args := appendScope(`
		SELECT r.agent_type_id, r.state, COUNT(*),
		       COALESCE(EXTRACT(EPOCH FROM (now() - MIN(r.started_at))) * 1000, 0)
		FROM runs r JOIN agent_types at ON at.id = r.agent_type_id
		WHERE r.tenant_id = $1 AND r.deleted_at IS NULL`, []any{tenantID}, scope)
	rows, err := r.pool.Query(ctx, query+` GROUP BY r.agent_type_id, r.state`, args...)
	if err != nil {
		return RunStateCounts{}, fmt.Errorf("count runs by state: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var agentTypeID, state string
		var n int64
		var runningMs float64
		if err := rows.Scan(&agentTypeID, &state, &n, &runningMs); err != nil {
			return RunStateCounts{}, fmt.Errorf("scan runs by state: %w", err)
		}
		out.Total += n
		out.ByState[RunState(state)] += n
		if slices.Contains(liveRunStates, RunState(state)) {
			out.Live += n
			out.LiveByAgentType[agentTypeID] += n
			out.LongestLiveMs = max(out.LongestLiveMs, int64(math.Round(runningMs)))
		}
	}
	if err := rows.Err(); err != nil {
		return RunStateCounts{}, fmt.Errorf("count runs by state: %w", err)
	}
	return out, nil
}

// stateStrings converts run states for a text[] query parameter.
func stateStrings(states []RunState) []string {
	out := make([]string, len(states))
	for i, s := range states {
		out[i] = string(s)
	}
	return out
}
