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

/** Effective per-million-token prices for a virtual model. */
export interface ModelObjectPricing {
  /** Effective per-1M token input cost (0 for free-tier; absent only when non_token_only). */
  effective_input_cost_per_1m?: number;
  /** Effective per-1M token output cost (0 for free-tier; absent only when non_token_only). */
  effective_output_cost_per_1m?: number;
  cache_read_cost_per_1m?: number;
  cache_write_cost_per_1m?: number;
  // Non-token dimensions — absent when zero:
  image_input_cost?: number;
  image_output_cost?: number;
  request_input_cost?: number;
  request_output_cost?: number;
  second_input_cost?: number;
  second_output_cost?: number;
  audio_input_cost_per_1m?: number;
  audio_output_cost_per_1m?: number;
  /**
   * True when the VM has no token-priced routes and pricing reflects
   * non-token dimensions only. Token cost fields will be absent.
   */
  non_token_only?: boolean;
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
  /** Effective pricing derived from the lowest-cost enabled route. Absent when no pricing data exists. */
  pricing?: ModelObjectPricing;
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

// ── Scopes Explain (policy-gateway) ──────────────────────────────────────────

export interface GrantDetail {
  grant_id: string;
  scopes: string[];
}

export interface RoleDetail {
  role: string;
  scopes: string[];
}

export interface ScopesExplainResponse {
  principal_type: string;
  principal_id: string;
  tenant_id: string;
  from_roles: RoleDetail[];
  from_grants: GrantDetail[];
  effective_scopes: string[];
  evaluated_at: string;
}

// ── Agents — /api/agents ──────────────────────────────────────────────────────

/** Execution framework the agent type targets. Mirrors Go: agents.Framework */
export type AgentFramework = 'claude-code' | 'codex' | 'opencode';

/** Lifecycle state of an AgentType resource. Mirrors Go: agents.AgentTypeState */
export type AgentTypeState = 'provisioning' | 'active' | 'deleting';

/**
 * An agent type's identity model — who the agent acts as. Chosen at creation
 * and immutable afterwards (docs/adr/2026-07-23-delegated-agent-identity.md).
 *   autonomous — acts as itself, on its own grants; automated sources may start it.
 *   delegated  — acts on behalf of the person who runs it, bounded by their
 *                access ∩ the agent's grants; startable by people only; runs
 *                are private to the starter + tenant admins.
 * Mirrors Go: agents.IdentityMode
 */
export type AgentIdentityMode = 'autonomous' | 'delegated';

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
  /**
   * Optional narrowing to specific upstream resources (the adapter's qualifier
   * dimension — repositories, namespaces). Omitted = all resources; the
   * compiled scopes stay unqualified. Mirrors Go: agents.Connector.
   */
  resources?: string[];
}

/**
 * A connection's resource-qualifier dimension, as declared by its adapter:
 * the dimension's name ("repository", "namespace") an attachment narrows to.
 * Mirrors Go: agents.ConnectionQualifierEntry.
 */
export interface ConnectionQualifierEntry {
  name: string;
  description: string;
}

/**
 * A connection available to attach as a connector, as surfaced by the egress
 * gateway's GET /connections. `actions` is the connection's scope ceiling — the
 * set of actions an agent may be granted through it. `baseline` lists the
 * action fragments the backend adds automatically at attachment (always
 * unqualified). `qualifier` is present only for adapters with a resource
 * dimension.
 */
export interface ConnectionCatalogEntry {
  name: string;
  adapter: string;
  actions: string[];
  baseline: string[];
  qualifier?: ConnectionQualifierEntry | null;
}

/**
 * One tool in the MCP tool-gateway catalog — the read-only projection the
 * gateway rebuilds on each introspection. `scope` is the mcp:<server>:<tool>:use
 * scope a principal must hold to call it; `server_slug` is the MCPServer name.
 */
export interface McpCatalogTool {
  tenant_id: string;
  server_slug: string;
  tool_name: string;
  scope: string;
  description: string;
}

/**
 * One MCP server visible to a tenant, from the tool-gateway's /v1/servers.
 * `caller_identity` is "forward" when the gateway acts AS the caller against
 * the upstream (global servers only); `global` is true for platform servers.
 */
export interface McpServer {
  name: string;
  display_name: string;
  description: string;
  transport: string;
  caller_identity: string;
  endpoint: string;
  global: boolean;
  tool_count: number;
  /** The distinct tool domains (first path segment), e.g. ["agents","policy"]. */
  domains: string[];
  /** RFC3339 timestamp of the last introspection, or "" if never. */
  last_synced: string;
  /** Coarse health: "healthy" | "stale" | "unknown". */
  status: string;
}

/**
 * An Environment is a prepackaged sandbox: a registered sandbox image, a size,
 * and the egress hosts and connectors of every agent it is attached to.
 * Mirrors Go: agents.Environment. See docs/adr/2026-10-03-sandbox-environments.md.
 */
export interface Environment {
  id: string;
  tenant_id: string;
  name: string;
  description: string;
  /** SandboxImage name in the tenant namespace. */
  sandbox_image: string;
  size: EnvironmentSize;
  /** Egress host patterns this environment grants (exact, "*", or "*.suffix"). */
  egress_hosts: string[];
  /** Credential connectors this environment grants. */
  connectors: Connector[];
  created_at: string;
  updated_at: string;
}

/** A sandbox's CPU, memory and local disk as Kubernetes quantities, e.g. "2", "4Gi". */
export interface EnvironmentSize {
  cpu: string;
  memory: string;
  disk: string;
}

/**
 * The platform's default and maximum environment sizes, from
 * GET /api/v1/environment-sizes. Mirrors Go: agents.EnvironmentSizes.
 */
export interface EnvironmentSizes {
  default: EnvironmentSize;
  max: EnvironmentSize;
}

/** Top-level response from GET /api/v1/environments. */
export interface EnvironmentsListResponse {
  environments: Environment[];
}

/** Request body for POST/PUT /api/v1/environments; PUT replaces in full. Empty image and size
 * fields take kora-base and the platform default, and a size above the maximum gets a 400. */
export interface EnvironmentRequest {
  name: string;
  description: string;
  sandbox_image: string;
  size: EnvironmentSize;
  egress_hosts: string[];
  connectors: Connector[];
}

/**
 * A sandbox image a platform team registered in the tenant. Only a valid image
 * can back an environment. Mirrors Go: agents.SandboxImageView.
 */
export interface SandboxImage {
  name: string;
  description: string;
  image: string;
  digest?: string;
  valid: boolean;
  /** Why an invalid image cannot be selected. */
  problem?: string;
  warm: boolean;
}

/** An environment attached to an agent type. Exactly one attached environment is the default. */
export interface AgentEnvironment {
  environment_id: string;
  is_default: boolean;
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
  /** Identity model — immutable after creation. Absent on pre-migration payloads (= autonomous). */
  identity_mode?: AgentIdentityMode;
  grant_scopes: string[];
  connectivity: Connectivity;
  connectors?: Connector[];
  /** Governed MCP tools the agent may call (mcp:<server>:<tool>:use). */
  mcp_tools?: string[];
  max_concurrent_runs_per_pod: number;
  /** Loop-guard thresholds; an absent field takes the platform default. */
  guardrails?: AgentTypeGuardrails;
  state: AgentTypeState;
  created_at: string;
  updated_at: string;
  /** Optional model identifier, e.g. "anthropic/claude-sonnet-4-6". */
  model_id?: string;
  /** Budget on the agent's policy grant. Present on detail reads when configured. */
  budget?: AgentBudget;
  /** Attached environments. None means the agent runs on the kora base image. */
  environments?: AgentEnvironment[];
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
  /** Number of tool calls made during this run, if available. */
  tool_calls?: number;
  /** LLM-generated one-sentence summary of the run's task intent, if available. */
  summary?: string | null;
  /**
   * Present when a trigger started this run. trigger_name and delivery_id are
   * omitted when unknown; absent entirely for human-started runs.
   */
  started_by?: {
    trigger_id: string;
    trigger_name?: string;
    delivery_id?: string;
  };
}

/**
 * A Run as served by the Next.js BFF run routes. `created_by_name` is resolved
 * by the BFF from the identity service (the same enrichment the analytics
 * principals route performs) and is NOT part of the Go response — it is null
 * or absent when the principal is not a tenant user (e.g. a webhook source).
 */
export interface RunWithAttribution extends Run {
  created_by_name?: string | null;
}

/** Top-level response from GET /agents/types. */
export interface AgentTypesListResponse {
  data: AgentType[];
}

/** Top-level response from GET /agents/runs. */
export interface RunsListResponse {
  data: Run[];
}

/**
 * Agent-type 7-day token breakdown and spend.
 * Mirrors Go: inferencegw.AgentTypeUsage
 */
export interface AgentTypeUsage {
  /** Agent slug: the last segment of the agent SPIFFE principal the gateway attributes usage to. */
  agent_slug: string;
  prompt_tokens: number;
  completion_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  total_tokens: number;
  estimated_cost_usd: number;
}

/** The reporting windows the agent insights routes accept. Mirrors Go: usagewindow.Period. */
export type AgentInsightsPeriod = '24h' | '7d' | '30d' | '90d';

/** One budget window of an agent. Mirrors Go: inferencegw.BudgetWindow. */
export interface AgentBudgetWindow {
  cadence: 'weekly' | 'monthly';
  limit_usd: number;
  /** Spend by agent in the window now. It can differ from the spend the limit check counts. */
  spend_usd: number;
  /** RFC 3339 time the window resets. */
  resets_at: string;
}

/** An agent's budget: a row per agent that has a window limit. Mirrors Go: inferencegw.AgentBudgetRow. */
export interface AgentBudgetStatus {
  agent_slug: string;
  per_run_limit_usd?: number;
  windows: AgentBudgetWindow[];
}

/**
 * Mirrors Go: inferencegw.AgentInsights plus what the BFF adds.
 * GET /api/agents/insights?period=24h|7d|30d|90d
 *
 * Agent traffic across the tenant, as aggregates per agent.
 */
export interface AgentInsightsResponse {
  period: AgentInsightsPeriod;
  usage: AgentTypeUsage[];
  total_cost_usd: number;
  /** Total for the window of the same length that ends where this one starts. */
  previous_total_usd: number;
  models: { model_id: string; estimated_cost_usd: number }[];
  /** Spend per bucket, oldest first, each with where it starts. */
  trend: { start: string; cost_usd: number }[];
  /** Absent when the policy bundle could not be read. */
  budgets?: AgentBudgetStatus[];
  /** Absent when orbit-agents could not count runs. */
  outcomes?: { completed: number; failed: number };
}

/**
 * Mirrors Go: agents.RunStats.
 * One agent's run counts and durations for a window, from orbit-agents.
 */
export interface AgentRunStats {
  period: AgentInsightsPeriod;
  /** Runs created in the window, split by whether a trigger started them. */
  total: number;
  triggered: number;
  manual: number;
  /** Runs that finished in the window. */
  completed: number;
  failed: number;
  /** Durations of the finished runs, in milliseconds. */
  durations: {
    finished: number;
    median_ms: number;
    average_ms: number;
    p95_ms: number;
    p99_ms: number;
    longest_ms: number;
    /** True when `buckets` cover 0 to p99 and slower runs are in `overflow`; false when they cover 0 to the longest run. */
    capped: boolean;
    range_ms: number;
    /** Ten equal-width counts across 0 to `range_ms`. */
    buckets: number[];
    overflow: number;
  };
}

/**
 * GET /api/agents/types/overview/{slug}?period=24h|7d|30d|90d
 *
 * What the agent Overview reads for one window. A part the caller may not read
 * or that could not be loaded is absent.
 */
export interface AgentOverviewResponse {
  period: AgentInsightsPeriod;
  /** The gateway insights of this one agent. Absent without agents:usage:read. */
  insights?: AgentInsightsResponse;
  runStats?: AgentRunStats;
}

/** Request body for POST /api/agents/types. */
export interface CreateAgentTypeRequest {
  name: string;
  slug: string;
  description?: string;
  system_prompt?: string;
  framework?: AgentFramework;
  /** Identity model. Omit for the default (autonomous). Immutable after creation. */
  identity_mode?: AgentIdentityMode;
  grant_scopes?: string[];
  connectivity?: Connectivity;
  connectors?: Connector[];
  /** Governed MCP tools the agent may call (mcp:<server>:<tool>:use). */
  mcp_tools?: string[];
  max_concurrent_runs_per_pod?: number;
  /** Loop-guard thresholds; an absent field takes the platform default. */
  guardrails?: AgentTypeGuardrails;
  model_id?: string;
  /** Budget applied to the agent's policy grant. */
  budget?: AgentBudget;
  /** Environments to attach. One is the default. Omit for none (the kora base image). */
  environments?: AgentEnvironment[];
}

/** Request body for POST /api/agents/types/:typeId/runs. */
export interface StartRunRequest {
  task?: string;
  provision_sandbox?: boolean;
}

/**
 * A saved agent-builder form. Not an agent type: it holds no grant, provisions
 * nothing, and is private to its creator. The payload is a partially-filled
 * CreateAgentTypeRequest with no invariants — the reader must tolerate
 * anything (docs/adr/2026-07-17-agent-builder.md §2). Mirrors Go:
 * agents.AgentTypeDraft.
 */
export interface AgentTypeDraft {
  id: string;
  tenant_id: string;
  created_by: string;
  payload: Partial<CreateAgentTypeRequest>;
  created_at: string;
  updated_at: string;
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

/** The hosts one attached environment contributes. Mirrors Go: agents.EnvironmentAccess. */
interface EnvironmentAccess {
  id: string;
  name: string;
  hosts: string[];
}

/**
 * The resolved, read-only view of what an agent type can actually reach, as
 * returned by GET /api/v1/agent-types/:id/effective-access. Folds together the
 * agent's own egress posture, its attached environments, and the auto-egress
 * implied by its connectors. Mirrors Go: agents.EffectiveAccess.
 */
export interface EffectiveAccess {
  /** true = open egress (reaches any host); `hosts` is empty in this case. */
  open: boolean;
  /** Hosts from the agent's own whitelist posture. */
  own_hosts: string[];
  /** What each attached environment contributes. */
  environments: EnvironmentAccess[];
  /** Union of every attached environment's hosts. */
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
  /** Environments to fold in. Omit or empty for none. */
  environments?: AgentEnvironment[];
}

/** Request body for PATCH /api/agents/types/:id. */
export interface UpdateAgentTypeRequest {
  name?: string;
  description?: string;
  framework?: AgentFramework;
  system_prompt?: string;
  max_concurrent_runs_per_pod?: number;
  /** Loop-guard thresholds; an absent field takes the platform default. */
  guardrails?: AgentTypeGuardrails;
  model_id?: string;
  grant_scopes?: string[];
  /** Budget update: omit = unchanged; both limits zero = remove. */
  budget?: AgentBudget;
  connectivity?: Connectivity;
  connectors?: Connector[];
  /** Governed MCP tool grants; replaces the set. Omit = unchanged; [] clears. */
  mcp_tools?: string[];
  /** Replaces the attached environments. Omit leaves them unchanged. [] detaches all. */
  environments?: AgentEnvironment[];
}

// ── Triggers ───────────────────────────────────────────────────────────────
//
// The Triggers feature starts agent runs automatically in response to external
// events. Three trigger kinds are supported: webhook, scheduled, and manual.
// Types mirror Go: internal/triggers.
// See docs/adr/2026-06-20-extended-trigger-types.md.

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
  /**
   * Inbound requests that failed authentication. These never become
   * deliveries: a bad signature is an unauthenticated request, so recording
   * one as a row would let anyone on the internet grow the table. Surfaced
   * here so an admin with a wrong signing secret can still see it.
   */
  rejection_count?: number;
  last_rejection_at?: string;
  last_rejection_reason?: string;
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

/** Trigger kind discriminant. */
export type TriggerKind = 'webhook' | 'scheduled' | 'manual';

/** Cron schedule configuration for scheduled triggers. */
export interface ScheduleConfig {
  /** Standard 5-field cron expression (e.g. "0 9 * * 1-5"). */
  cron_expression: string;
  /** IANA timezone name (e.g. "Australia/Sydney"). */
  timezone: string;
  /**
   * Whether a fire missed while the platform was down runs on recovery.
   * Off by default: an agent doing real work twice is worse than not doing it,
   * and a long outage on a frequent schedule would dump a burst of runs.
   */
  catch_up?: boolean;
}

/** Field types a manual trigger may declare. */
export type InputFieldType = 'text' | 'number' | 'boolean' | 'choice';

/**
 * One input a manual trigger accepts. The same declaration renders the console
 * form and validates the API call, so the two cannot drift.
 */
export interface InputField {
  name: string;
  type: InputFieldType;
  label?: string;
  description?: string;
  required?: boolean;
  default?: string;
  /** Permitted values when type is "choice". */
  choices?: string[];
}

/** Input declaration for a manual trigger. */
export interface ManualConfig {
  inputs?: InputField[];
}

/** Overflow behavior when a trigger is at its concurrency ceiling. */
export type TriggerOverflow = 'wait' | 'replace' | 'refuse';

/** Upcoming fire returned by the schedule preview. */
export interface SchedulePreviewFire {
  at: string;
  label: string;
}

/** Response to POST /api/v1/triggers/schedule-preview. */
export interface SchedulePreviewResponse {
  next_fires: SchedulePreviewFire[];
  timezone: string;
}

/**
 * Who may fire a webhook trigger. A sender passes when its login or its
 * association is listed; both lists empty means anyone.
 */
export interface SenderRestriction {
  logins?: string[];
  associations?: string[];
}

/**
 * GET /trigger-sources/:id/event-types and GET /trigger-providers/:provider/event-types.
 * Declared types come from the provider's schema, observed ones from what a
 * source has sent; the provider endpoint has no observed types.
 */
export interface TriggerEventTypes {
  declared: string[];
  observed?: string[];
  /** Association values a sender restriction may name; empty when the provider identifies no sender. */
  associations: string[];
}

/** One field of a provider event, from its schema. Mirrors Go: triggers.Field. */
export interface TriggerField {
  /** Dotted path, e.g. `comment.body`, or `meta.event_type`. */
  path: string;
  group: string;
  description?: string;
  /** Allowed values, when the schema enumerates them. */
  enum?: string[];
  /** A family whose last segment is data, e.g. `commonLabels.<name>`. */
  pattern?: boolean;
}

/** A trigger binds a firing condition to an agent. Mirrors Go: triggers.Trigger. */
export interface Trigger {
  id: string;
  tenant_id: string;
  /** "webhook" | "scheduled" | "manual" */
  kind: TriggerKind;
  /** Present for webhook triggers only. */
  source_id?: string;
  name: string;
  agent_type_id: string;
  agent_slug: string;
  /** Present for webhook triggers only. */
  event_type?: string;
  /** field → allowed values; a delivery matches when every clause is satisfied. Webhook only. */
  match_condition?: Record<string, string[]>;
  /** field → substrings, matched ignoring case; omitted when unset. Webhook only. */
  match_contains?: Record<string, string[]>;
  prompt_template: string;
  /** Present for scheduled triggers only. */
  schedule_config?: ScheduleConfig;
  /** Present for manual triggers only: the inputs this trigger accepts. */
  manual_config?: ManualConfig;
  /** Webhook triggers only. */
  sender_restriction?: SenderRestriction;
  /**
   * What this trigger starts. "agent" is the only kind today; triggers are the
   * front door for starting work, and what gets started is a separate concern.
   */
  target_kind?: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;

  /** Admission control: concurrency ceiling and what happens beyond it. */
  max_parallel?: number;
  overflow?: TriggerOverflow;
  max_queue_depth?: number;

  /** Delivery safety. Timeout bounds starting the run, not running it. */
  timeout_seconds?: number;
  dedupe_enabled?: boolean;
  autopause_enabled?: boolean;
  consecutive_failures?: number;
  /**
   * Set when the platform disabled this trigger itself after sustained
   * failure, which the console shows differently from one a person turned off.
   */
  auto_paused_at?: string;
  /** Deliveries admitted and queued for this trigger, not yet started. */
  pending_count?: number;
}

/** Request body for POST /api/triggers. */
export interface TriggerCreateRequest {
  /**
   * Trigger kind. Defaults to "webhook" when omitted for backward compatibility.
   */
  kind?: TriggerKind;
  /** Required for webhook triggers. */
  source_id?: string;
  name: string;
  agent_type_id: string;
  /**
   * Ignored by the backend, which resolves the canonical slug from
   * `agent_type_id` server-side. Optional and retained only for backward
   * compatibility — do not rely on it being honored.
   */
  agent_slug?: string;
  /** Required for webhook triggers. */
  event_type?: string;
  match_condition?: Record<string, string[]>;
  match_contains?: Record<string, string[]>;
  prompt_template: string;
  /** Required for scheduled triggers. */
  schedule_config?: ScheduleConfig;
  /** For manual triggers: the inputs the console form and the API validate. */
  manual_config?: ManualConfig;
  /** Webhook only. */
  sender_restriction?: SenderRestriction;
  // ── Admission control (all optional; defaults per kind when omitted) ──
  /** Concurrency ceiling, 1..16. */
  max_parallel?: number;
  /** What happens to an arrival beyond the ceiling. */
  overflow?: 'wait' | 'replace' | 'refuse';
  /** Queue bound when overflow is 'wait', 1..500. */
  max_queue_depth?: number;
  /** How long a queued delivery may wait before it is dropped, 5..600 seconds. */
  timeout_seconds?: number;
  dedupe_enabled?: boolean;
  autopause_enabled?: boolean;
}

/** Request body for PATCH /api/triggers/:id. */
export interface TriggerUpdateRequest {
  name?: string;
  event_type?: string;
  match_condition?: Record<string, string[]>;
  /** Replaces the contains filters when present; `{}` removes them. */
  match_contains?: Record<string, string[]>;
  prompt_template?: string;
  enabled?: boolean;
  /** For scheduled triggers: update the cron schedule. */
  schedule_config?: ScheduleConfig;
  /** Webhook only. Replaces the restriction; both lists empty removes it. */
  sender_restriction?: SenderRestriction;
  /**
   * Suppress a repeat of a delivery already seen. Omit to leave unchanged: a
   * PATCH that only renames a trigger must not re-arm its safety settings.
   */
  dedupe_enabled?: boolean;
  /** Disable the trigger automatically after repeated consecutive failures. */
  autopause_enabled?: boolean;
  // ── Admission tuning (same bounds as create; applies to the next delivery) ──
  max_parallel?: number;
  overflow?: 'wait' | 'replace' | 'refuse';
  max_queue_depth?: number;
  timeout_seconds?: number;
}

/** Request body for POST /api/triggers/:id/fire (manual triggers only). */
export interface TriggerFireRequest {
  /** Values substituted into {{ var.key }} placeholders in the prompt template. */
  variables?: Record<string, string>;
}

/** Response from POST /api/triggers/:id/fire. */
export interface TriggerFireResponse {
  delivery_id: string;
  run_id: string;
}

/** A durable record of a trigger delivery and its outcome. Mirrors Go: triggers.Delivery. */
export interface TriggerDelivery {
  id: string;
  tenant_id: string;
  /** "webhook" | "scheduled" | "manual" */
  kind: TriggerKind;
  /** Present for webhook deliveries. */
  source_id?: string;
  /** The matched trigger (all kinds). */
  trigger_id?: string;
  /** Denormalised name of the matched trigger, so a feed can name it without a lookup. */
  trigger_name?: string;
  provider_delivery_id?: string;
  event_type?: string;
  /**
   * What happened to the arrival itself. Derived from the per-trigger outcomes
   * in `runs` once the fan-out completes, never authored independently.
   *
   * verified | dispatched | skipped | failed | duplicate | missed.
   *
   * Deliberately absent: failed authentication. A bad signature is an
   * unauthenticated request, counted on the source rather than recorded here,
   * so every delivery is an arrival that authenticated.
   */
  status: string;
  run_id?: string;
  error?: string;
  payload_excerpt?: string;
  /** Full captured body, nulled once past the tenant's retention window. */
  payload?: string;
  payload_bytes?: number;
  payload_purged?: boolean;
  /** Why a non-dispatch arrival ended as it did (duplicate, missed). */
  reason?: string;
  /** The delivery this one re-runs; a redelivery references, never edits. */
  redelivery_of?: string;
  /** Principal ID of the caller who fired a manual trigger. */
  fired_by?: string;
  /** Cron tick time for scheduled trigger deliveries. */
  scheduled_at?: string;
  received_at: string;
  /** One entry per matched trigger — the complete per-trigger dispatch record. */
  runs?: TriggerDeliveryRun[];
}

/** A single matched-trigger dispatch within a delivery. */
export interface TriggerDeliveryRun {
  id: string;
  delivery_id: string;
  trigger_id: string;
  /** Denormalised name of this entry's trigger. */
  trigger_name?: string;
  /** Present when the run started; absent when the dispatch failed. */
  run_id?: string;
  /**
   * What happened for this trigger:
   * dispatched | failed | filtered | denied | superseded | dropped | paused.
   *
   * This is the level admission control, auto-pause and the console's
   * per-trigger badges act on.
   */
  status: string;
  error?: string;
  created_at: string;
}

/**
 * An agent type's loop-guard thresholds (docs/adr/2026-09-29-agent-run-guardrails.md).
 * Each warn is at least 1 and each pause is greater than its warn.
 */
export interface AgentTypeGuardrails {
  repeated_call_warn?: number;
  repeated_call_pause?: number;
  wait_rearm_warn?: number;
  wait_rearm_pause?: number;
}
