package agents

import (
	"context"
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
	UpdateAgentType(ctx context.Context, at AgentType) error
	// SetAgentTypeState does a compare-and-swap: only updates if current state matches from.
	SetAgentTypeState(ctx context.Context, id string, from, to AgentTypeState) error
	DeleteAgentType(ctx context.Context, id string) error
}

// RunRepository is the persistence interface for run operations, events, and messages.
type RunRepository interface {
	CreateRun(ctx context.Context, r Run) error
	GetRun(ctx context.Context, id string) (*Run, error)
	ListRuns(ctx context.Context, tenantID string, agentTypeID string) ([]Run, error)
	// SetRunState does a compare-and-swap on runs.state.
	SetRunState(ctx context.Context, id string, from, to RunState) error
	// AssignPod sets pod_ordinal and transitions pending→starting atomically.
	AssignPod(ctx context.Context, runID string, podOrdinal int) error
	DeleteRun(ctx context.Context, id string) error
	// DeleteRunsByAgentType force-deletes all runs for the given agent type. Used during
	// agent type deletion to clear the FK reference before the agent type row is removed.
	DeleteRunsByAgentType(ctx context.Context, agentTypeID string) error
	// ActiveRunsPerPod returns active run counts keyed by pod ordinal for an agent type.
	ActiveRunsPerPod(ctx context.Context, agentTypeID string) (map[int]int, error)
	// ReapStaleRuns marks runs that have been stuck in active or starting state
	// for longer than staleDuration as failed. Returns the IDs of reaped runs.
	ReapStaleRuns(ctx context.Context, staleDuration time.Duration) ([]string, error)

	// ── Events ────────────────────────────────────────────────────────────────

	AppendRunEvent(ctx context.Context, e RunEvent) (int64, error)
	ListRunEvents(ctx context.Context, runID string, afterSeqNo int64) ([]RunEvent, error)

	// ── Messages ──────────────────────────────────────────────────────────────

	AppendRunMessage(ctx context.Context, m RunMessage) error
}

// Repository is the combined persistence interface for orbit-agents.
// It is retained for backward-compatibility; most new code should depend on
// AgentTypeRepository or RunRepository directly.
type Repository interface {
	AgentTypeRepository
	RunRepository
}
