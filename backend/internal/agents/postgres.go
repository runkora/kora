package agents

import (
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
	"github.com/jackc/pgx/v5/pgxpool"

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

// ErrIdentityModeImmutable is returned when an update attempts to change an
// agent type's identity mode — it is fixed at creation
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 1). Handlers map it to a 400.
var ErrIdentityModeImmutable = errors.New("identity_mode is immutable: it is set at creation and cannot be changed")

// ErrRunNotAcceptingMessages is returned when a message is sent to a run that is not in a state that accepts messages.
var ErrRunNotAcceptingMessages = errors.New("run not accepting messages")

// ErrRunStarting is returned when a message is sent to a run the scheduler has not started yet.
var ErrRunStarting = errors.New("run is starting")

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
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO agent_types
				(id, tenant_id, name, slug, description, system_prompt, framework, grant_scopes, max_concurrent_runs_per_pod, state, model_id, created_at, updated_at, connectivity, connectors, mcp_tools, identity_mode, guardrails, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15::jsonb,$16,$17,$18::jsonb,$19)`,
			at.ID, at.TenantID, at.Name, at.Slug, at.Description, at.SystemPrompt, string(at.Framework),
			at.GrantScopes, at.MaxConcurrentRunsPerPod, string(at.State), at.ModelID, now, now, string(connectivityJSON), string(connectorsJSON), at.MCPTools, string(at.IdentityMode.OrDefault()),
			guardrailsColumn(at.Guardrails), at.CreatedBy,
		)
		if err != nil {
			if isUniqueViolation(err) {
				return ErrSlugTaken
			}
			return fmt.Errorf("insert agent_type: %w", err)
		}
		return writeAgentEnvironments(ctx, tx, at.ID, at.Environments)
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
		FROM agent_types WHERE id = $1`, id)
	return scanAgentType(row)
}

func (r *PostgresRepository) GetAgentTypeBySlug(ctx context.Context, tenantID, slug string) (*AgentType, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+agentTypeColumns+`
		FROM agent_types WHERE tenant_id = $1 AND slug = $2`, tenantID, slug)
	return scanAgentType(row)
}

func (r *PostgresRepository) ListAgentTypes(ctx context.Context, tenantID string) ([]AgentType, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+agentTypeColumns+`
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
	// identity_mode is deliberately absent from the SET list: it is immutable
	// after creation (the service layer rejects changes; the storage layer never
	// writes it on update).
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE agent_types
			SET name=$1, slug=$2, description=$3, system_prompt=$4, framework=$5, grant_scopes=$6, max_concurrent_runs_per_pod=$7, model_id=$8, updated_at=$9, connectivity=$10::jsonb, connectors=$11::jsonb, mcp_tools=$12, guardrails=$13::jsonb
			WHERE id=$14`,
			at.Name, at.Slug, at.Description, at.SystemPrompt, string(at.Framework), at.GrantScopes, at.MaxConcurrentRunsPerPod, at.ModelID, time.Now().UTC(), string(connectivityJSON), string(connectorsJSON), at.MCPTools,
			guardrailsColumn(at.Guardrails), at.ID,
		)
		if err != nil {
			return fmt.Errorf("update agent_type: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return writeAgentEnvironments(ctx, tx, at.ID, at.Environments)
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
	// Trigger attribution is written at insert and never updated: it records why
	// the run exists, which cannot change after it starts.
	var triggerID, triggerName, deliveryID *string
	if run.StartedBy != nil {
		triggerID = storage.NullIfZero(run.StartedBy.TriggerID)
		triggerName = storage.NullIfZero(run.StartedBy.TriggerName)
		deliveryID = storage.NullIfZero(run.StartedBy.DeliveryID)
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO runs
			(id, tenant_id, agent_type_id, state, pod_ordinal, created_by, created_at, updated_at,
			 trigger_id, trigger_name, delivery_id, initial_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		run.ID, run.TenantID, run.AgentTypeID, string(RunStatePending), -1, run.CreatedBy, now, now,
		triggerID, triggerName, deliveryID, run.InitialMessage,
	)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

// selectRun joins agent_types so every run read carries the agent's
// identity_mode (runs always reference an existing agent type — DeleteAgentType
// force-deletes its runs first, so the inner join never drops rows).
const selectRun = `SELECT ` + runColumns + ` FROM runs r JOIN agent_types at ON at.id = r.agent_type_id`

const runColumns = `
	r.id, r.tenant_id, r.agent_type_id, r.state, r.pod_ordinal, r.created_by, r.started_at, r.completed_at, r.created_at, r.updated_at, r.summary, r.delegation_jti, at.identity_mode,
	r.trigger_id, r.trigger_name, r.delivery_id, r.environment_id`

func (r *PostgresRepository) GetRun(ctx context.Context, id string) (*Run, error) {
	row := r.pool.QueryRow(ctx, selectRun+` WHERE r.id=$1`, id)
	return scanRun(row)
}

// ListRuns returns up to q.Limit runs in scope, newest first, after q.After.
// The scope is part of the query, so a page holds only runs the caller may read.
func (r *PostgresRepository) ListRuns(ctx context.Context, tenantID string, q RunListQuery) ([]Run, error) {
	query, args := appendScope(selectRun+` WHERE r.tenant_id=$1`, []any{tenantID}, q.Scope)
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

func (r *PostgresRepository) SetRunState(ctx context.Context, id string, from, to RunState) error {
	now := time.Now().UTC()
	var tag interface{ RowsAffected() int64 }
	var err error
	if to == RunStateCompleted || to == RunStateFailed || to == RunStateBudgetExhausted {
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
		        WHERE  state = 'pending' AND claimed_at IS NULL AND not_before <= $2
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

func (r *PostgresRepository) DeleteRunsByAgentType(ctx context.Context, agentTypeID string) ([]DeletedRun, error) {
	rows, err := r.pool.Query(ctx, `DELETE FROM runs WHERE agent_type_id=$1 RETURNING id, delegation_jti`, agentTypeID)
	if err != nil {
		return nil, fmt.Errorf("delete runs for agent type: %w", err)
	}
	defer rows.Close()

	var deleted []DeletedRun
	for rows.Next() {
		var d DeletedRun
		if scanErr := rows.Scan(&d.ID, &d.DelegationJTI); scanErr != nil {
			return nil, fmt.Errorf("scan deleted run: %w", scanErr)
		}
		deleted = append(deleted, d)
	}
	return deleted, rows.Err()
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

// runsActiveByStateFrameworkQuery aggregates every pod-occupying run by state,
// its agent type's framework, tenant, and agent type, across all tenants, for
// the orbit_agents_runs_active gauge. Scoped to the OccupiesPod states the
// same way activeRunsPerPodQuery is: a "runs active" gauge counting every run
// that ever reached a terminal state would not be a gauge, it would be a
// growing total. tenant_id/agent_type_id grouping added by the 2026-10-20
// amendment to docs/adr/2026-10-07-domain-metrics-instrumentation.md.
var runsActiveByStateFrameworkQuery = fmt.Sprintf(`
	SELECT r.state, a.framework, r.tenant_id, r.agent_type_id, COUNT(*)
	FROM runs r
	JOIN agent_types a ON a.id = r.agent_type_id
	WHERE r.state IN (%s)
	GROUP BY r.state, a.framework, r.tenant_id, r.agent_type_id`, podOccupancySQLList())

// CountRunsByStateAndFramework aggregates currently-active runs by state and
// agent framework. Read only by the orbit_agents_runs_active observable gauge
// callback (internal/agents/metrics.go), not by any request path — deliberately
// not part of the RunRepository interface, which every mock in this package's
// tests would otherwise have to implement for a method they do not exercise.
func (r *PostgresRepository) CountRunsByStateAndFramework(ctx context.Context) ([]RunStateFrameworkCount, error) {
	rows, err := r.pool.Query(ctx, runsActiveByStateFrameworkQuery)
	if err != nil {
		return nil, fmt.Errorf("count runs by state and framework: %w", err)
	}
	defer rows.Close()

	var out []RunStateFrameworkCount
	for rows.Next() {
		var (
			state, framework, tenantID, agentTypeID string
			count                                   int64
		)
		if err := rows.Scan(&state, &framework, &tenantID, &agentTypeID, &count); err != nil {
			return nil, fmt.Errorf("scan run state count: %w", err)
		}
		out = append(out, RunStateFrameworkCount{
			State:       RunState(state),
			Framework:   AgentFramework(framework),
			TenantID:    tenantID,
			AgentTypeID: agentTypeID,
			Count:       count,
		})
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
//   - pending → failed (never queued, or no scheduler took it)
//   - idle → completed  (session-manager died after a clean turn; idle timeout
//     never fired because the pod restarted)
func (r *PostgresRepository) ReapStaleRuns(ctx context.Context, staleDuration time.Duration) (failed, completed []string, err error) {
	now := time.Now().UTC()
	cutoff := now.Add(-staleDuration)
	rows, err := r.pool.Query(ctx, `
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

// AppendRunEvent records an event. A caller that also publishes the event sets
// CreatedAt, so the live stream and a later replay carry the same time.
func (r *PostgresRepository) AppendRunEvent(ctx context.Context, e RunEvent) (int64, error) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = eventTime()
	}
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

// agentTypeColumns is the projection scanAgentType expects, in its order.
// Every agent_type read selects it rather than repeating the list: pgx matches
// columns to scan destinations positionally and errors on a count mismatch, so
// a query that drifts from the scanner fails on every row at runtime rather
// than at compile time.
// The last column aggregates the agent's environment links, default first.
const agentTypeColumns = `id, tenant_id, name, slug, description, system_prompt, framework, grant_scopes, max_concurrent_runs_per_pod, state, model_id, created_at, updated_at, connectivity, connectors, mcp_tools, identity_mode, guardrails, created_by, activated_at,
	COALESCE((SELECT jsonb_agg(jsonb_build_object('environment_id', ae.environment_id, 'is_default', ae.is_default) ORDER BY ae.is_default DESC, ae.environment_id)
		FROM agent_environments ae WHERE ae.agent_type_id = agent_types.id), '[]'::jsonb)`

func scanAgentType(s scanner) (*AgentType, error) {
	var at AgentType
	var state, framework, identityMode string
	var connectivityJSON, connectorsJSON, guardrailsJSON, environmentsJSON []byte
	var activatedAt *time.Time
	err := s.Scan(
		&at.ID, &at.TenantID, &at.Name, &at.Slug, &at.Description, &at.SystemPrompt, &framework,
		&at.GrantScopes, &at.MaxConcurrentRunsPerPod, &state, &at.ModelID, &at.CreatedAt, &at.UpdatedAt, &connectivityJSON, &connectorsJSON, &at.MCPTools, &identityMode, &guardrailsJSON, &at.CreatedBy, &activatedAt, &environmentsJSON,
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
	var triggerID, triggerName, deliveryID *string
	err := s.Scan(append([]any{
		&r.ID, &r.TenantID, &r.AgentTypeID, &state, &r.PodOrdinal,
		&r.CreatedBy, &r.StartedAt, &r.CompletedAt, &r.CreatedAt, &r.UpdatedAt,
		&r.Summary, &r.DelegationJTI, &identityMode,
		&triggerID, &triggerName, &deliveryID, &r.EnvironmentID,
	}, extra...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan run: %w", err)
	}
	r.State = RunState(state)
	r.IdentityMode = IdentityMode(identityMode).OrDefault()
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
		WHERE r.tenant_id = $1
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
	WHERE r.tenant_id = $1
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
		WHERE r.tenant_id = $1 AND r.created_at >= $2 AND r.created_at < $3`,
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
		WHERE r.tenant_id = $1`, []any{tenantID}, scope)
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
