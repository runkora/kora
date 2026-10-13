// ── Display-layer types for the Agents feature ────────────────────────────────
//
// AgentTypeRow and RunRow are the shapes consumed by all UI components.
// Raw API shapes (AgentType, Run) only appear at the service layer boundary.

import type {
  AgentEnvironment,
  AgentType,
  AgentBudget,
  AgentTypeGuardrails,
  AgentIdentityMode,
  RunWithAttribution,
  AgentTypeState,
  RunState,
  Connectivity,
  Connector,
} from '@orbit/shared/api';

export type {
  AgentEnvironment,
  AgentIdentityMode,
  AgentTypeState,
  RunState,
  Connectivity,
  Connector,
};

/** The states of a run in flight. An idle run waits for a message and is not live. */
export const LIVE_RUN_STATES: readonly RunState[] = ['pending', 'starting', 'active', 'completing'];

/** Runs per page on the agent's Runs tab. */
export const RUNS_PAGE_SIZE = 50;

export interface AgentTypeRow {
  id: string;
  name: string;
  slug: string;
  description: string;
  /** Human-readable label, e.g. "Claude Code" for framework "claude-code". */
  framework: string;
  /** Raw framework value for badge variant selection. */
  frameworkKey: string;
  /** Identity model: acts as itself (autonomous) or as the person who runs it (delegated). Immutable. */
  identityMode: AgentIdentityMode;
  state: AgentTypeState;
  grantScopes: string[];
  /** Outbound egress posture (none / whitelist / open) + whitelist hosts. */
  connectivity: Connectivity;
  /** Attached credential connections + the actions granted through each. */
  connectors: Connector[];
  /** Governed MCP tools the agent may call (mcp:<server>:<tool>:use). */
  mcpTools: string[];
  maxConcurrentRunsPerPod: number;
  /** Loop-guard thresholds as stored; absent fields take the platform defaults. */
  guardrails: AgentTypeGuardrails;
  systemPrompt: string;
  createdAt: string;
  updatedAt: string;
  /** Model identifier, e.g. "anthropic/claude-sonnet-4-6". */
  modelId: string;
  /** Budget on the agent's policy grant (present on detail reads when configured). */
  budget: AgentBudget | null;
  /** Attached environments. One is the default. Empty means the kora base image. */
  environments: AgentEnvironment[];
}

export interface RunRow {
  id: string;
  agentTypeId: string;
  agentTypeName: string;
  agentTypeSlug: string;
  state: RunState;
  createdBy: string;
  /** Display name of the person who started the run (BFF-resolved), or null when unresolvable. */
  createdByName: string | null;
  /** The trigger that started this run, when one did. */
  startedByTrigger: {
    triggerId: string;
    triggerName: string | null;
    deliveryId: string | null;
  } | null;
  /** True when the owning agent is delegated — the run acted on the starter's behalf and is private to them. */
  delegated: boolean;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
  /** Elapsed ms: completedAt - startedAt, or now - startedAt for active runs, or null if not started. */
  durationMs: number | null;
  /** Number of tool calls made during this run, or null if not available. */
  toolCalls: number | null;
  /** LLM-generated one-sentence summary of the run's task intent, or null if not available. */
  summary: string | null;
}

// ── Transformers ──────────────────────────────────────────────────────────────

const FRAMEWORK_LABELS: Record<string, string> = {
  'claude-code': 'Claude Code',
  codex: 'Codex',
  opencode: 'OpenCode',
};

export function toAgentTypeRow(a: AgentType): AgentTypeRow {
  return {
    id: a.id,
    name: a.name,
    slug: a.slug,
    description: a.description,
    framework: FRAMEWORK_LABELS[a.framework] ?? a.framework,
    frameworkKey: a.framework,
    identityMode: a.identity_mode ?? 'autonomous',
    state: a.state,
    grantScopes: a.grant_scopes,
    connectivity: a.connectivity ?? { mode: 'none' },
    connectors: a.connectors ?? [],
    mcpTools: a.mcp_tools ?? [],
    maxConcurrentRunsPerPod: a.max_concurrent_runs_per_pod,
    guardrails: a.guardrails ?? {},
    systemPrompt: a.system_prompt,
    createdAt: a.created_at,
    updatedAt: a.updated_at,
    modelId: a.model_id ?? '',
    budget: a.budget ?? null,
    environments: a.environments ?? [],
  };
}

// indexAgentTypes builds an id→row lookup so toRunRow is O(1) per run instead of
// scanning the full agent-type list (O(runs × types)) on every render/refresh.
export function indexAgentTypes(agentTypes: AgentTypeRow[]): Map<string, AgentTypeRow> {
  return new Map(agentTypes.map((a) => [a.id, a]));
}

export function toRunRow(r: RunWithAttribution, agentTypeById: Map<string, AgentTypeRow>): RunRow {
  const agent = agentTypeById.get(r.agent_type_id);
  const startedMs = r.started_at ? new Date(r.started_at).getTime() : null;
  const endMs = r.completed_at ? new Date(r.completed_at).getTime() : null;
  const durationMs = startedMs !== null ? (endMs ?? Date.now()) - startedMs : null;
  return {
    id: r.id,
    agentTypeId: r.agent_type_id,
    agentTypeName: agent?.name ?? 'Unknown',
    agentTypeSlug: agent?.slug ?? r.agent_type_id,
    state: r.state,
    createdBy: r.created_by,
    createdByName: r.created_by_name ?? null,
    startedByTrigger: r.started_by
      ? {
          triggerId: r.started_by.trigger_id,
          triggerName: r.started_by.trigger_name ?? null,
          deliveryId: r.started_by.delivery_id ?? null,
        }
      : null,
    delegated: agent?.identityMode === 'delegated',
    startedAt: r.started_at,
    completedAt: r.completed_at,
    createdAt: r.created_at,
    durationMs,
    toolCalls: r.tool_calls ?? null,
    summary: r.summary ?? null,
  };
}

// ── Utilities ─────────────────────────────────────────────────────────────────

export function formatDuration(ms: number | null): string {
  if (ms === null) return '—';
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

/**
 * Returns a human-readable relative time string for a given ISO timestamp.
 * Examples: "just now", "2 minutes ago", "3 hours ago", "4 days ago".
 */
export function formatRelativeTime(iso: string): string {
  const diffMs = Date.now() - new Date(iso).getTime();
  const diffS = Math.floor(diffMs / 1000);
  if (diffS < 60) return 'just now';
  const diffM = Math.floor(diffS / 60);
  if (diffM < 60) return `${diffM} minute${diffM !== 1 ? 's' : ''} ago`;
  const diffH = Math.floor(diffM / 60);
  if (diffH < 24) return `${diffH} hour${diffH !== 1 ? 's' : ''} ago`;
  const diffD = Math.floor(diffH / 24);
  return `${diffD} day${diffD !== 1 ? 's' : ''} ago`;
}

// ── Loop guardrails ───────────────────────────────────────────────────────────

/** Platform defaults for the loop-guard thresholds; they match the agents API. */
export const LOOP_GUARDRAIL_DEFAULTS = {
  repeatedCallWarn: 4,
  repeatedCallPause: 8,
  waitRearmWarn: 6,
  waitRearmPause: 12,
} as const;

/**
 * The loop thresholds as form values. Each pair has a switch: off sends no
 * override, so the agent type takes the platform defaults.
 */
export interface LoopGuardrailValues {
  repeatedCallOn: boolean;
  repeatedCallWarn: string;
  repeatedCallPause: string;
  waitRearmOn: boolean;
  waitRearmWarn: string;
  waitRearmPause: string;
}

/** Form values for stored thresholds: a pair is on when either of its fields is set. */
export function loopGuardrailValues(g: AgentTypeGuardrails | undefined): LoopGuardrailValues {
  const d = LOOP_GUARDRAIL_DEFAULTS;
  return {
    repeatedCallOn: g?.repeated_call_warn != null || g?.repeated_call_pause != null,
    repeatedCallWarn: String(g?.repeated_call_warn ?? d.repeatedCallWarn),
    repeatedCallPause: String(g?.repeated_call_pause ?? d.repeatedCallPause),
    waitRearmOn: g?.wait_rearm_warn != null || g?.wait_rearm_pause != null,
    waitRearmWarn: String(g?.wait_rearm_warn ?? d.waitRearmWarn),
    waitRearmPause: String(g?.wait_rearm_pause ?? d.waitRearmPause),
  };
}

/** The same rules the API applies to a pair that is on: warn is a whole number of at least 1, pause is greater than warn. */
export function loopGuardrailErrors(
  v: LoopGuardrailValues,
): Partial<Record<'repeatedCall' | 'waitRearm', string>> {
  const errors: Partial<Record<'repeatedCall' | 'waitRearm', string>> = {};
  const check = (warnText: string, pauseText: string): string | undefined => {
    const warn = Number(warnText);
    const pause = Number(pauseText);
    if (!Number.isInteger(warn) || warn < 1)
      return 'Warn after must be a whole number of at least 1.';
    if (!Number.isInteger(pause) || pause <= warn)
      return 'Pause after must be greater than warn after.';
    return undefined;
  };
  const repeated = v.repeatedCallOn && check(v.repeatedCallWarn, v.repeatedCallPause);
  if (repeated) errors.repeatedCall = repeated;
  const waits = v.waitRearmOn && check(v.waitRearmWarn, v.waitRearmPause);
  if (waits) errors.waitRearm = waits;
  return errors;
}

/** The API payload: the pairs that are switched on. */
export function loopGuardrailsPayload(v: LoopGuardrailValues): AgentTypeGuardrails {
  const out: AgentTypeGuardrails = {};
  if (v.repeatedCallOn) {
    out.repeated_call_warn = Number(v.repeatedCallWarn);
    out.repeated_call_pause = Number(v.repeatedCallPause);
  }
  if (v.waitRearmOn) {
    out.wait_rearm_warn = Number(v.waitRearmWarn);
    out.wait_rearm_pause = Number(v.waitRearmPause);
  }
  return out;
}

// ── Attached environments ─────────────────────────────────────────────────────
// The API requires exactly one attached environment to be the default.
// These helpers keep the draft list in that shape as the user edits it.

/** Attaches or detaches an environment. The first attached becomes the default. */
export function toggleEnvironment(list: AgentEnvironment[], id: string): AgentEnvironment[] {
  if (!list.some((e) => e.environment_id === id)) {
    return [...list, { environment_id: id, is_default: list.length === 0 }];
  }
  const rest = list.filter((e) => e.environment_id !== id);
  if (rest.length > 0 && !rest.some((e) => e.is_default)) {
    return rest.map((e, i) => ({ ...e, is_default: i === 0 }));
  }
  return rest;
}

/** Marks one attached environment as the default. */
export function setDefaultEnvironment(list: AgentEnvironment[], id: string): AgentEnvironment[] {
  return list.map((e) => ({ ...e, is_default: e.environment_id === id }));
}
