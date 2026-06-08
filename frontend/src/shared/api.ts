// ── Shared API types ─────────────────────────────────────────────────────────
//
// These interfaces mirror Go backend JSON response shapes exactly.
// Field names match json tags — do not rename without updating the backend.

export interface HealthResponse {
  status: string;
  /** Human-readable service name, e.g. "orbit-identity". */
  service: string;
  timestamp: string;
}

// ── Inference Gateway — /v1/models ────────────────────────────────────────────

/**
 * Content type that a model can accept as input or produce as output.
 * Mirrors Go: inferencegw.Modality
 */
export type Modality = 'text' | 'image' | 'embedding' | 'audio' | 'video';

/**
 * Wire protocol used to call a chat model.
 * Mirrors Go: inferencegw.APIDialect
 */
export type APIDialect = 'openai' | 'anthropic' | 'responses';

/**
 * Endpoint family a model belongs to.
 * Mirrors Go: inferencegw.ModelCapability
 *
 * Three-layer taxonomy (matches docs/adr/2026-03-31-tenant-registration-and-oidc-provider-binding.md / docs/adr/2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md):
 *   ModelCapability — what endpoint family?    (chat, embedding, image, …)
 *   APIDialect      — which wire format?       (openai, anthropic) [chat only]
 *   Modality        — what physical I/O types? (text, image, embedding, …)
 */
export type ModelCapability =
  | 'chat' // POST /v1/chat/completions or /v1/messages
  | 'embedding' // POST /v1/embeddings (handler not yet active)
  | 'image' // POST /v1/images/generations (handler not yet active)
  | 'video' // async video generation (no standard endpoint yet)
  | 'rerank' // POST /v1/rerank (handler not yet active)
  | 'audio'; // TTS / live audio (no standard endpoint yet)

/** A tool scope definition returned by GET /api/agents/tool-scopes. */
export interface ToolScopeDefinition {
  scope: string;
  description: string;
  tools: string[];
}

/** Raw model object returned by the inference gateway. */
export interface ModelObject {
  id: string;
  object: string;
  created: number;
  owned_by: string;
  /** Canonical vendor/model-family identifier used for policy scope (e.g. "anthropic", "google", "meta"). */
  family: string;
  display_name: string;
  status: 'available' | 'unavailable';
  aliases: string[];
  input_modalities: Modality[];
  output_modalities: Modality[];
  /** Only populated for models with CapabilityChat. */
  dialects: APIDialect[];
  capabilities: ModelCapability[];
}

/** Top-level response from GET /v1/models. */
export interface ModelsListResponse {
  object: 'list';
  data: ModelObject[];
}

// ── Identity — /api/v1/identity/tokens ───────────────────────────────────────

/**
 * A Personal Access Token summary as returned by LIST.
 * The raw token value is never included — it is only returned once on creation.
 * Mirrors Go: identity.PATSummary
 */
export interface PAT {
  id: string;
  name: string;
  scopes: string[];
  expires_at: string;
  created_at: string;
  revoked_at: string | null;
}

/** Request body for creating a new PAT. Mirrors Go: identity.CreatePATRequest */
export interface CreatePATRequest {
  name: string;
  /** Optional. Defaults to 90 days on the backend. Max 365. */
  expires_in_days?: number;
  scopes: string[];
}

/**
 * Response from POST /tokens — includes the raw `orb_<jwt>` token.
 * This is the only time the token is returned; it cannot be retrieved again.
 * Mirrors Go: identity.CreatePATResponse
 */
export interface CreatePATResponse {
  id: string;
  name: string;
  /** Raw PAT string (`orb_<jwt>`). Store it securely — shown once only. */
  token: string;
  scopes: string[];
  expires_at: string;
  created_at: string;
}

/** Request body for PATCH /tokens/{id}/scopes. Mirrors Go: identity.UpdatePATScopesRequest */
export interface UpdatePATScopesRequest {
  scopes: string[];
}

/** Top-level response from GET /tokens. Mirrors Go: identity.ListPATsResponse */
export interface ListPATsResponse {
  tokens: PAT[];
}

// ── Agents — /api/agents ──────────────────────────────────────────────────────

/** Execution framework the agent type targets. Mirrors Go: agents.Framework */
export type AgentFramework = 'claude-code' | 'codex' | 'opencode';

/** Lifecycle state of an AgentType resource. Mirrors Go: agents.AgentTypeState */
export type AgentTypeState = 'provisioning' | 'active' | 'deleting';

/** Lifecycle state of a single Run. Mirrors Go: agents.RunState */
export type RunState =
  | 'pending'
  | 'starting'
  | 'active'
  | 'idle'
  | 'completing'
  | 'completed'
  | 'failed'
  | 'budget_exhausted';

/**
 * An agent type's outbound egress posture — the reachability gate for its
 * sandboxes. Mirrors Go: agents.ConnectivityMode.
 *   none      — no external egress (deny-by-default)
 *   whitelist — egress only to the listed host patterns
 *   open      — egress to any host
 */
export type ConnectivityMode = 'none' | 'whitelist' | 'open';

/**
 * Connectivity posture. `hosts` is consulted only in whitelist mode and holds
 * host patterns (exact, "*", or "*.suffix"), IPv4/DNS only.
 * Mirrors Go: agents.Connectivity
 */
export interface Connectivity {
  mode: ConnectivityMode;
  hosts?: string[];
}

/**
 * An AgentType defines the configuration template for a class of agents.
 * Mirrors Go: agents.AgentType
 */
export interface AgentType {
  id: string;
  tenant_id: string;
  name: string;
  slug: string;
  description: string;
  system_prompt: string;
  framework: AgentFramework;
  tool_scopes: string[];
  grant_scopes: string[];
  connectivity: Connectivity;
  max_concurrent_runs_per_pod: number;
  state: AgentTypeState;
  created_at: string;
  updated_at: string;
  /** Optional model identifier, e.g. "anthropic/claude-sonnet-4-6". */
  model_id?: string;
  /** Budget on the agent's policy grant. Present on detail reads when configured. */
  budget?: AgentBudget;
}

/**
 * Spending configuration applied to the agent's policy grant.
 * Mirrors Go: agents.AgentBudget. Both limits zero/absent = no budget.
 */
export interface AgentBudget {
  /** Per-window ceiling in USD (0/absent = none). */
  limit_usd?: number;
  /** "weekly" or "monthly" (default monthly). */
  reset_cadence?: string;
  /** Per-run ceiling in USD for agent runs (0/absent = none). */
  per_run_limit_usd?: number;
}

/**
 * A single execution of an AgentType.
 * Mirrors Go: agents.Run
 */
export interface Run {
  id: string;
  tenant_id: string;
  agent_type_id: string;
  state: RunState;
  pod_ordinal: number;
  created_by: string;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
  updated_at: string;
  /** Total tokens consumed by this run, if available. */
  tokens_used?: number;
  /** Number of tool calls made during this run, if available. */
  tool_calls?: number;
}

/** Top-level response from GET /agents/types. */
export interface AgentTypesListResponse {
  data: AgentType[];
}

/** Top-level response from GET /agents/runs. */
export interface RunsListResponse {
  data: Run[];
}

/** Request body for POST /api/agents/types. */
export interface CreateAgentTypeRequest {
  name: string;
  slug: string;
  description?: string;
  system_prompt?: string;
  framework?: AgentFramework;
  tool_scopes?: string[];
  grant_scopes?: string[];
  connectivity?: Connectivity;
  max_concurrent_runs_per_pod?: number;
  model_id?: string;
  /** Budget applied to the agent's policy grant. */
  budget?: AgentBudget;
}

/** Request body for POST /api/agents/types/:typeId/runs. */
export interface StartRunRequest {
  task?: string;
  provision_sandbox?: boolean;
}

/** Request body for PATCH /api/agents/types/:id. */
export interface UpdateAgentTypeRequest {
  name?: string;
  description?: string;
  framework?: AgentFramework;
  system_prompt?: string;
  max_concurrent_runs_per_pod?: number;
  model_id?: string;
  tool_scopes?: string[];
  grant_scopes?: string[];
  /** Budget update: omit = unchanged; both limits zero = remove. */
  budget?: AgentBudget;
  connectivity?: Connectivity;
}
