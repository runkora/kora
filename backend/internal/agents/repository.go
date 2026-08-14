package agents

import (
	"context"
	"encoding/json"
	"time"
)

// AgentTypeRepository is the persistence interface for agent type operations.
type AgentTypeRepository interface {
	CreateAgentType(ctx context.Context, at AgentType) error
	GetAgentType(ctx context.Context, id string) (*AgentType, error)
	GetAgentTypeBySlug(ctx context.Context, tenantID, slug string) (*AgentType, error)
	ListAgentTypes(ctx context.Context, tenantID string) ([]AgentType, error)
	// ListAllAgentTypeSlugs returns the distinct set of agent type slugs across all tenants.
	// Used by scope registration to build the per-agent-type vocabulary.
	ListAllAgentTypeSlugs(ctx context.Context) ([]string, error)
	// ListAgentTypesByEnvironment returns the agent types in a tenant that
	// reference the given environment. Used to re-reconcile their grants when the
	// environment changes (the reference model).
	ListAgentTypesByEnvironment(ctx context.Context, tenantID, environmentID string) ([]AgentType, error)
	UpdateAgentType(ctx context.Context, at AgentType) error
	// SetAgentTypeState does a compare-and-swap: only updates if current state matches from.
	SetAgentTypeState(ctx context.Context, id string, from, to AgentTypeState) error
	DeleteAgentType(ctx context.Context, id string) error
}

// RunRepository is the persistence interface for run operations, events, and messages.
type RunRepository interface {
	CreateRun(ctx context.Context, r Run) error
	GetRun(ctx context.Context, id string) (*Run, error)
	// ListRuns returns a tenant's runs newest first, optionally narrowed to one
	// agent type. limit caps the row count; limit <= 0 returns every match.
	// vis applies the delegated-run visibility rule INSIDE the query (Decision 6
	// of docs/adr/2026-07-23-delegated-agent-identity.md): unless vis.SeeAll,
	// runs of delegated agent types are returned only where created_by equals
	// vis.Subject. Autonomous-agent runs are never filtered.
	ListRuns(ctx context.Context, tenantID string, agentTypeID string, vis RunVisibility, limit int) ([]Run, error)
	// SetRunState does a compare-and-swap on runs.state.
	SetRunState(ctx context.Context, id string, from, to RunState) error
	// AssignPod sets pod_ordinal and transitions pending→starting atomically.
	AssignPod(ctx context.Context, runID string, podOrdinal int) error
	// MarkRunFailed unconditionally transitions a run to the failed terminal state
	// and stamps completed_at. Used to clean up synchronously when a run's start
	// fails after the row was created, so it never lingers in pending/starting
	// waiting for the stale-run reaper.
	MarkRunFailed(ctx context.Context, id string) error
	// SetRunDelegationJTI stamps the delegation issuance minted for a delegated
	// run at start (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 3),
	// so terminal-state transitions can revoke it.
	SetRunDelegationJTI(ctx context.Context, runID, jti string) error
	DeleteRun(ctx context.Context, id string) error
	// DeleteRunsByAgentType force-deletes all runs for the given agent type and
	// returns them as (id, delegation JTI) pairs. Used during agent type deletion
	// to clear the FK reference before the agent type row is removed; the returned
	// IDs let the caller clean up each run's sandbox pod (named sandbox-{runID}),
	// which is not covered by DeprovisionAgentType, and the JTIs let it revoke
	// each run's delegation issuance.
	DeleteRunsByAgentType(ctx context.Context, agentTypeID string) ([]DeletedRun, error)
	// ActiveRunsPerPod returns active run counts keyed by pod ordinal for an agent type.
	ActiveRunsPerPod(ctx context.Context, agentTypeID string) (map[int]int, error)
	// ReapStaleRuns marks runs stuck in transient states for longer than
	// staleDuration. Returns the IDs partitioned by terminal state:
	//   - failed:    runs transitioned active|starting → failed
	//   - completed: runs transitioned idle → completed (idle timeout missed
	//                due to a session-manager restart)
	ReapStaleRuns(ctx context.Context, staleDuration time.Duration) (failed, completed []string, err error)

	// ── Events ────────────────────────────────────────────────────────────────

	AppendRunEvent(ctx context.Context, e RunEvent) (int64, error)
	ListRunEvents(ctx context.Context, runID string, afterSeqNo int64) ([]RunEvent, error)

	// SetRunSummary persists the LLM-generated task summary for a run.
	// Called asynchronously after run start; a failure does not affect the run.
	SetRunSummary(ctx context.Context, runID, summary string) error

	// ── Messages ──────────────────────────────────────────────────────────────

	AppendRunMessage(ctx context.Context, m RunMessage) error
}

// DraftRepository is the persistence interface for agent type draft operations.
// A draft has no invariants: the payload is opaque JSON. Ownership (tenant +
// created_by) is enforced by the service layer from verified claims;
// ListDrafts additionally filters at the query so a caller only ever sees
// their own drafts.
type DraftRepository interface {
	CreateDraft(ctx context.Context, d AgentTypeDraft) error
	GetDraft(ctx context.Context, id string) (*AgentTypeDraft, error)
	ListDrafts(ctx context.Context, tenantID, createdBy string) ([]AgentTypeDraft, error)
	// UpdateDraft fully replaces the draft payload and bumps updated_at.
	UpdateDraft(ctx context.Context, id string, payload json.RawMessage) error
	DeleteDraft(ctx context.Context, id string) error
	// DeleteDraftsOlderThan removes drafts whose updated_at predates cutoff and
	// returns how many were deleted. Used by the draft reaper.
	DeleteDraftsOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// EnvironmentRepository is the persistence interface for environment operations.
type EnvironmentRepository interface {
	CreateEnvironment(ctx context.Context, env Environment) error
	GetEnvironment(ctx context.Context, tenantID, id string) (*Environment, error)
	ListEnvironments(ctx context.Context, tenantID string) ([]Environment, error)
	UpdateEnvironment(ctx context.Context, env Environment) error
	DeleteEnvironment(ctx context.Context, tenantID, id string) error
}

// Repository is the combined persistence interface for orbit-agents.
// It is retained for backward-compatibility; most new code should depend on
// AgentTypeRepository, RunRepository, or EnvironmentRepository directly.
type Repository interface {
	AgentTypeRepository
	RunRepository
	EnvironmentRepository
	DraftRepository
}
