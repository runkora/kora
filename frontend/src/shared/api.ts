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
 * A connector attaches a registered credential connection to an agent type and
 * the actions the agent may invoke through it. Compiles (server-side) to
 * connection:<connection>:<action> grants — the credential parallel to how
 * connectivity compiles hosts to egress:<host>:connect.
 * Mirrors Go: agents.Connector
 */
export interface Connector {
  connection: string;
  actions: string[];
}

/**
 * A connection available to attach as a connector, as surfaced by the egress
 * gateway's GET /connections. `actions` is the connection's scope ceiling — the
 * set of actions an agent may be granted through it.
 */
export interface ConnectionCatalogEntry {
  name: string;
  adapter: string;
  actions: string[];
}

/**
 * An Environment is a tenant-scoped, UI-managed bundle of egress hosts and
 * credential connectors an agent type can reference. Attaching an environment to
 * an agent type folds its hosts and connectors into the agent's effective
 * egress + connection grants.
 * Mirrors Go: agents.Environment
 */
export interface Environment {
  id: string;
  tenant_id: string;
  name: string;
  description: string;
  /** Egress host patterns this environment grants (exact, "*", or "*.suffix"). */
  egress_hosts: string[];
  /** Credential connectors this environment grants. */
  connectors: Connector[];
  created_at: string;
  updated_at: string;
}

/** Top-level response from GET /api/v1/environments. */
export interface EnvironmentsListResponse {
  environments: Environment[];
}

/**
 * Request body for POST/PUT /api/v1/environments. PUT is a FULL replace — every
 * field is sent.
 */
export interface EnvironmentRequest {
  name: string;
  description: string;
  egress_hosts: string[];
  connectors: Connector[];
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
  connectors?: Connector[];
  max_concurrent_runs_per_pod: number;
  state: AgentTypeState;
  created_at: string;
  updated_at: string;
  /** Optional model identifier, e.g. "anthropic/claude-sonnet-4-6". */
  model_id?: string;
  /** Budget on the agent's policy grant. Present on detail reads when configured. */
  budget?: AgentBudget;
  /** Optional environment this agent type references (its hosts + connectors are folded in). */
  environment_id?: string | null;
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
  connectors?: Connector[];
  max_concurrent_runs_per_pod?: number;
  model_id?: string;
  /** Budget applied to the agent's policy grant. */
  budget?: AgentBudget;
  /** Environment to attach (UUID). Omit/empty for none. */
  environment_id?: string;
}

/** Request body for POST /api/agents/types/:typeId/runs. */
export interface StartRunRequest {
  task?: string;
  provision_sandbox?: boolean;
}

/**
 * Per-connector auto-egress entry — the hosts a connection's adapter implies.
 * Mirrors Go: agents.ConnectorEgress.
 */
export interface ConnectorEgress {
  connection: string;
  adapter: string;
  hosts: string[];
}

/**
 * The resolved, read-only view of what an agent type can actually reach, as
 * returned by GET /api/v1/agent-types/:id/effective-access. Folds together the
 * agent's own egress posture, its referenced environment, and the auto-egress
 * implied by its connectors. Mirrors Go: agents.EffectiveAccess.
 */
export interface EffectiveAccess {
  /** true = open egress (reaches any host); `hosts` is empty in this case. */
  open: boolean;
  /** Hosts from the agent's own whitelist posture. */
  own_hosts: string[];
  environment_id?: string;
  environment_name?: string;
  /** Hosts contributed by the referenced environment. */
  environment_hosts: string[];
  /** Per-connector auto-egress, grouped by connection. */
  connector_egress: ConnectorEgress[];
  /** Effective connectors (own + environment), with their granted actions. */
  connectors: Connector[];
  /** Deduplicated, sorted union of all reachable hosts (empty when open). */
  hosts: string[];
}

/**
 * Request body for POST /api/v1/agent-types/effective-access — the live-preview
 * endpoint. Resolves the effective-access view for a *draft* (unsaved)
 * connectivity config so the resolved panel can update as the user edits.
 * Mirrors Go: agents.EffectiveAccessPreviewRequest.
 */
export interface EffectiveAccessPreviewRequest {
  connectivity: Connectivity;
  connectors: Connector[];
  /** Environment to fold in (UUID). Omit/null/"" for none. */
  environment_id?: string | null;
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
  connectors?: Connector[];
  /** Environment to attach (UUID); empty string ("") detaches it. Omit = unchanged. */
  environment_id?: string;
}

// ── Triggers ───────────────────────────────────────────────────────────────
//
// The Triggers feature starts agent runs automatically in response to external
// events. The webhook trigger is the first kind. Types mirror Go:
// internal/triggers. See docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md.

/**
 * A webhook source is a registered inbound origin of webhook deliveries (e.g. a
 * GitHub repository). Mirrors Go: triggers.WebhookSource.
 */
export interface WebhookSource {
  id: string;
  tenant_id: string;
  name: string;
  /** Source adapter id, e.g. "github". */
  provider: string;
  config?: Record<string, unknown>;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

/**
 * Response to source create/rotate. Mirrors Go: triggers.SourceResponse.
 * `secret` and `webhook_path` are returned ONCE (at create or secret rotation).
 */
export interface WebhookSourceResponse extends WebhookSource {
  /** Inbound path to configure on the provider; combine with the public base URL. */
  webhook_path: string;
  /** HMAC signing secret — shown once; never returned on reads. */
  secret?: string;
}

/** Request body for POST /api/trigger-sources. */
export interface WebhookSourceCreateRequest {
  name: string;
  provider: string;
  config?: Record<string, unknown>;
}

/** Request body for PATCH /api/trigger-sources/:id. */
export interface WebhookSourceUpdateRequest {
  enabled?: boolean;
  rotate_secret?: boolean;
}

/** A trigger binds a source event to an agent. Mirrors Go: triggers.Trigger. */
export interface Trigger {
  id: string;
  tenant_id: string;
  source_id: string;
  name: string;
  agent_type_id: string;
  agent_slug: string;
  event_type: string;
  /** field → allowed values; a delivery matches when every clause is satisfied. */
  match_condition?: Record<string, string[]>;
  prompt_template: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

/** Request body for POST /api/triggers. */
export interface TriggerCreateRequest {
  source_id: string;
  name: string;
  agent_type_id: string;
  agent_slug: string;
  event_type: string;
  match_condition?: Record<string, string[]>;
  prompt_template: string;
}

/** Request body for PATCH /api/triggers/:id. */
export interface TriggerUpdateRequest {
  name?: string;
  event_type?: string;
  match_condition?: Record<string, string[]>;
  prompt_template?: string;
  enabled?: boolean;
}

/** A durable record of an inbound delivery and its outcome. Mirrors Go: triggers.Delivery. */
export interface TriggerDelivery {
  id: string;
  tenant_id: string;
  source_id: string;
  provider_delivery_id?: string;
  event_type?: string;
  /** received | verified | dispatched | skipped | failed. */
  status: string;
  trigger_id?: string;
  run_id?: string;
  error?: string;
  payload_excerpt?: string;
  received_at: string;
}
