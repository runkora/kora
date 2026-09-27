// ── Agents API — client-side BFF calls ───────────────────────────────────────
//
// All functions call the BFF routes added in Plan 2. Never throws — callers
// pattern-match on the returned `error` field.

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type {
  AgentType,
  AgentTypeDraft,
  AgentTypeUsage,
  ConnectionCatalogEntry,
  CreateAgentTypeRequest,
  Environment,
  McpCatalogTool,
  McpServer,
  ModelObject,
  Run,
  RunStats,
  StartRunRequest,
  ToolScopeDefinition,
} from '@orbit/shared/api';
import { toAgentTypeRow, toRunRow } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

// ── fetchAgentTypes ───────────────────────────────────────────────────────────

export async function fetchAgentTypes(): Promise<{ data: AgentTypeRow[]; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.list(), { cache: 'no-store' });
  } catch {
    return { data: [], error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      data: [],
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentType[] };
  return { data: (body.data ?? []).map(toAgentTypeRow), error: null };
}

// ── fetchAgentTypesUsage ──────────────────────────────────────────────────────
// Returns the 7-day token + spend aggregate per agent type, keyed by agent type
// id for direct lookup. Never throws; on error returns an empty map.

export async function fetchAgentTypesUsage(): Promise<{
  byAgentType: Record<string, AgentTypeUsage>;
  error: string | null;
}> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.usageAll(), { cache: 'no-store' });
  } catch {
    return { byAgentType: {}, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      byAgentType: {},
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentTypeUsage[] };
  const byAgentType: Record<string, AgentTypeUsage> = {};
  for (const u of body.data ?? []) byAgentType[u.agent_type_id] = u;
  return { byAgentType, error: null };
}

// ── fetchRunStats ─────────────────────────────────────────────────────────────

/**
 * Fetches run counts and average completed duration, computed server-side, so
 * the dashboard never has to fetch every run to summarize them. Pass
 * `agentTypeId` to scope to one agent type; omit for a tenant-wide summary.
 */
export async function fetchRunStats(
  agentTypeId?: string,
): Promise<{ data: RunStats | null; error: string | null }> {
  const qs = agentTypeId ? `?agent_type_id=${encodeURIComponent(agentTypeId)}` : '';
  let res: Response;
  try {
    res = await fetch(`${ApiRoute.runs.stats()}${qs}`, { cache: 'no-store' });
  } catch {
    return { data: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      data: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const data = (await res.json()) as RunStats;
  return { data, error: null };
}

// ── fetchStartRun ─────────────────────────────────────────────────────────────

export async function fetchStartRun(
  agentTypeId: string,
  payload: StartRunRequest,
): Promise<{ run: RunRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.runs.start(agentTypeId), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { run: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      run: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: Run };
  const run = toRunRow(body.data, new Map());
  return { run, error: null };
}

// ── createAgentType ───────────────────────────────────────────────────────────

export async function createAgentType(
  payload: CreateAgentTypeRequest,
): Promise<{ agentType: AgentTypeRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.list(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { agentType: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      agentType: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentType };
  return { agentType: toAgentTypeRow(body.data), error: null };
}

// ── fetchAgentType ────────────────────────────────────────────────────────────
// Single agent type by id or slug. Used by the builder's success panel to poll
// provisioning state after create.

export async function fetchAgentType(
  idOrSlug: string,
): Promise<{ agentType: AgentTypeRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.detail(idOrSlug), { cache: 'no-store' });
  } catch {
    return { agentType: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      agentType: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentType };
  return { agentType: toAgentTypeRow(body.data), error: null };
}

// ── Drafts ────────────────────────────────────────────────────────────────────
// The builder's persistence. A draft is a saved form, private to the caller;
// it holds no grant and provisions nothing (docs/adr/2026-07-17-agent-builder.md §2).

export async function fetchDrafts(): Promise<{ data: AgentTypeDraft[]; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.drafts(), { cache: 'no-store' });
  } catch {
    return { data: [], error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      data: [],
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentTypeDraft[] };
  return { data: body.data ?? [], error: null };
}

export async function fetchDraft(
  id: string,
): Promise<{ draft: AgentTypeDraft | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.draft(id), { cache: 'no-store' });
  } catch {
    return { draft: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      draft: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentTypeDraft };
  return { draft: body.data, error: null };
}

export async function createDraft(
  payload: Partial<CreateAgentTypeRequest>,
): Promise<{ draft: AgentTypeDraft | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.drafts(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ payload }),
    });
  } catch {
    return { draft: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      draft: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentTypeDraft };
  return { draft: body.data, error: null };
}

export async function updateDraft(
  id: string,
  payload: Partial<CreateAgentTypeRequest>,
): Promise<{ draft: AgentTypeDraft | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.draft(id), {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ payload }),
    });
  } catch {
    return { draft: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      draft: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentTypeDraft };
  return { draft: body.data, error: null };
}

export async function deleteDraft(id: string): Promise<{ error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.draft(id), { method: 'DELETE' });
  } catch {
    return { error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return { error: (body as { message?: string }).message ?? `Request failed (${res.status})` };
  }
  return { error: null };
}

// ── Builder catalogues ────────────────────────────────────────────────────────
// The supporting data the builder loads once on open (docs/adr/2026-07-17-agent-builder.md §3).
// Same no-throw contract as everything above, so a failed catalogue surfaces
// as an error on the step that needs it instead of an empty control.

async function fetchCatalogue<T>(
  url: string,
  extract: (body: unknown) => T,
  empty: T,
): Promise<{ data: T; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(url, { cache: 'no-store' });
  } catch {
    return { data: empty, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      data: empty,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as unknown;
  return { data: extract(body), error: null };
}

export function fetchModels(): Promise<{ data: ModelObject[]; error: string | null }> {
  return fetchCatalogue(
    ApiRoute.models.GET(),
    // Agents run a conversation loop, so only chat-capable models qualify —
    // an embeddings model in this list is a nonsense option.
    (b) =>
      ((b as { data?: ModelObject[] }).data ?? []).filter(
        (m) => m.status === 'available' && m.capabilities.includes('chat'),
      ),
    [],
  );
}

export function fetchToolScopes(): Promise<{
  data: ToolScopeDefinition[];
  error: string | null;
}> {
  return fetchCatalogue(
    ApiRoute.agentTypes.toolScopes(),
    (b) => (b as { data?: ToolScopeDefinition[] }).data ?? [],
    [],
  );
}

export function fetchConnections(): Promise<{
  data: ConnectionCatalogEntry[];
  error: string | null;
}> {
  return fetchCatalogue(
    ApiRoute.agentTypes.connectors(),
    (b) => (b as { data?: ConnectionCatalogEntry[] }).data ?? [],
    [],
  );
}

export function fetchMcpCatalogue(): Promise<{
  data: { servers: McpServer[]; catalog: McpCatalogTool[] };
  error: string | null;
}> {
  return fetchCatalogue(
    ApiRoute.agentTypes.mcpTools(),
    (b) => {
      const body = b as { servers?: McpServer[]; catalog?: McpCatalogTool[] };
      return { servers: body.servers ?? [], catalog: body.catalog ?? [] };
    },
    { servers: [], catalog: [] },
  );
}

export function fetchEnvironments(): Promise<{ data: Environment[]; error: string | null }> {
  return fetchCatalogue(
    ApiRoute.environments.list(),
    (b) => (b as { data?: Environment[] }).data ?? [],
    [],
  );
}
