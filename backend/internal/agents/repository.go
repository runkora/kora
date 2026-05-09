package agents

import "context"

// Repository is the persistence interface for orbit-agents.
// All methods are tenant-scoped; callers must supply tenantID where relevant.
type Repository interface {
	// ── Agent Types ──────────────────────────────────────────────────────────

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

	// ── Runs ─────────────────────────────────────────────────────────────────

	CreateRun(ctx context.Context, r Run) error
	GetRun(ctx context.Context, id string) (*Run, error)
	ListRuns(ctx context.Context, tenantID string, agentTypeID string) ([]Run, error)
	// SetRunState does a compare-and-swap on runs.state.
	SetRunState(ctx context.Context, id string, from, to RunState) error
	// AssignPod sets pod_ordinal and transitions pending→starting atomically.
	AssignPod(ctx context.Context, runID string, podOrdinal int) error
	DeleteRun(ctx context.Context, id string) error
	// ActiveRunsPerPod returns active run counts keyed by pod ordinal for an agent type.
	ActiveRunsPerPod(ctx context.Context, agentTypeID string) (map[int]int, error)

	// ── Events ────────────────────────────────────────────────────────────────

	AppendRunEvent(ctx context.Context, e RunEvent) (int64, error)
	ListRunEvents(ctx context.Context, runID string, afterSeqNo int64) ([]RunEvent, error)

	// ── Messages ──────────────────────────────────────────────────────────────

	AppendRunMessage(ctx context.Context, m RunMessage) error
}
