// ── Display-layer types for the Agents feature ────────────────────────────────
//
// AgentTypeRow and RunRow are the shapes consumed by all UI components.
// Raw API shapes (AgentType, Run) only appear at the service layer boundary.

import type { AgentType, Run, AgentTypeState, RunState } from '@orbit/shared/api';

export type { AgentTypeState, RunState };

export interface AgentTypeRow {
  id: string;
  name: string;
  slug: string;
  description: string;
  /** Human-readable label, e.g. "Claude Code" for framework "claude-code". */
  framework: string;
  /** Raw framework value for badge variant selection. */
  frameworkKey: string;
  state: AgentTypeState;
  toolScopes: string[];
  grantScopes: string[];
  maxConcurrentRunsPerPod: number;
  systemPrompt: string;
  createdAt: string;
  updatedAt: string;
  /** Model identifier, e.g. "anthropic/claude-sonnet-4-6". */
  modelId: string;
}

export interface RunRow {
  id: string;
  agentTypeId: string;
  agentTypeName: string;
  agentTypeSlug: string;
  state: RunState;
  createdBy: string;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
  /** Elapsed ms: completedAt - startedAt, or now - startedAt for active runs, or null if not started. */
  durationMs: number | null;
  /** Total tokens consumed by this run, or null if not available. */
  tokensUsed: number | null;
  /** Number of tool calls made during this run, or null if not available. */
  toolCalls: number | null;
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
    state: a.state,
    toolScopes: a.tool_scopes,
    grantScopes: a.grant_scopes,
    maxConcurrentRunsPerPod: a.max_concurrent_runs_per_pod,
    systemPrompt: a.system_prompt,
    createdAt: a.created_at,
    updatedAt: a.updated_at,
    modelId: a.model_id ?? 'anthropic/claude-sonnet-4-6',
  };
}

export function toRunRow(r: Run, agentTypes: AgentTypeRow[]): RunRow {
  const agent = agentTypes.find((a) => a.id === r.agent_type_id);
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
    startedAt: r.started_at,
    completedAt: r.completed_at,
    createdAt: r.created_at,
    durationMs,
    tokensUsed: r.tokens_used ?? null,
    toolCalls: r.tool_calls ?? null,
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
