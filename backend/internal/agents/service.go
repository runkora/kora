package agents

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/clear-route/orbit/internal/platform/clients/sessionmanager"
)

// agentsTracer is the package-level OpenTelemetry tracer used by both services.
var agentsTracer = otel.Tracer("orbit.agents")

// k8sProvisioner abstracts K8sProvisioner so it can be replaced in tests.
type k8sProvisioner interface {
	ProvisionAgentType(ctx context.Context, at AgentType, namespace string) error
	DeprovisionAgentType(ctx context.Context, at AgentType, namespace string) error
	EnsureAgentGrant(ctx context.Context, at AgentType, namespace string) error
	GetAgentBudget(ctx context.Context, at AgentType, namespace string) (*AgentBudget, error)
	WaitForStatefulSetReady(ctx context.Context, slug, namespace string) error
	ReplicaCount(ctx context.Context, slug, namespace string) (int, error)
	ProvisionSandbox(ctx context.Context, run Run, agentTypeSlug, namespace string) (string, error)
	DeleteSandbox(ctx context.Context, runID, namespace string) error
}

// smClient abstracts sessionmanager.Client so it can be replaced in tests.
type smClient interface {
	CreateSession(ctx context.Context, podURL string, req sessionmanager.CreateSessionRequest) error
	SendMessage(ctx context.Context, podURL, runID, content string) error
	InterruptSession(ctx context.Context, podURL, runID string) error
	CloseSession(ctx context.Context, podURL, runID string) error
}

// ServiceConfig holds runtime parameters. Kept for use in cmd/orbit-agents/main.go.
type ServiceConfig struct {
	MaxConcurrentRunsPerPod int
	TenantNamespaceFn       func(tenantID string) string // maps tenant ID → k8s namespace
	// StaleRunTimeout is how long a run may stay in active/starting without a state update
	// before the background reaper marks it failed. Defaults to 30 minutes if zero.
	StaleRunTimeout time.Duration
	// ReaperInterval controls how often the reaper polls. Defaults to 5 minutes if zero.
	// Override in tests to speed up reaper cycles.
	ReaperInterval time.Duration
}

// ── Request types ─────────────────────────────────────────────────────────────

type CreateAgentTypeRequest struct {
	TenantID                string         `json:"tenant_id"`
	Name                    string         `json:"name"`
	Slug                    string         `json:"slug"`
	Description             string         `json:"description"`
	SystemPrompt            string         `json:"system_prompt"`
	Framework               AgentFramework `json:"framework"`
	ToolScopes              []string       `json:"tool_scopes"`
	GrantScopes             []string       `json:"grant_scopes"`
	Connectivity            *Connectivity  `json:"connectivity"`
	Connectors              []Connector    `json:"connectors"`
	MaxConcurrentRunsPerPod int            `json:"max_concurrent_runs_per_pod"`
	ModelID                 *string        `json:"model_id"`
	// Budget is applied to the agent's policy grant (nil = no budget).
	Budget *AgentBudget `json:"budget"`
}

type UpdateAgentTypeRequest struct {
	ID                      string          `json:"-"`
	Name                    *string         `json:"name"`
	Description             *string         `json:"description"`
	SystemPrompt            *string         `json:"system_prompt"`
	Framework               *AgentFramework `json:"framework"`
	MaxConcurrentRunsPerPod *int            `json:"max_concurrent_runs_per_pod"`
	ModelID                 *string         `json:"model_id"`
	ToolScopes              []string        `json:"tool_scopes"`
	GrantScopes             []string        `json:"grant_scopes"`
	// Budget updates the agent's grant budget. nil = unchanged; both limits
	// zero = remove the budget.
	Budget       *AgentBudget  `json:"budget"`
	Connectivity *Connectivity `json:"connectivity"`
	Connectors   []Connector   `json:"connectors"`
}

type StartRunRequest struct {
	TenantID       string `json:"tenant_id"`
	AgentTypeID    string `json:"agent_type_id"`
	InitialMessage string `json:"initial_message"`
	// ProvisionSandbox, when true, eagerly warms a sandbox pod for the run so the
	// first tool call does not pay cold-start latency. Best-effort; failures log.
	ProvisionSandbox bool `json:"provision_sandbox"`
	// CreatedBy is intentionally absent: the run creator is always derived from the
	// verified platform token in ctx, never from the request body.
}
