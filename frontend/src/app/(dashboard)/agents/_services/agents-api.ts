// ── Agents API — client-side BFF calls ───────────────────────────────────────
//
// All functions call the BFF routes added in Plan 2. Never throws — callers
// pattern-match on the returned `error` field.

import { loadErrorMessage } from '@runkora/lib/load-error';
import { ApiRoute } from '@runkora/lib/routing/api-routes';
import type {
  AgentInsightsPeriod,
  AgentInsightsResponse,
  AgentRunStats,
  AgentType,
  AgentTypeDraft,
  ConnectionCatalogEntry,
  CreateAgentTypeRequest,
  Environment,
  McpCatalogTool,
  McpServer,
  ModelObject,
  Run,
  RunState,
  RunsPageResponse,
  StartRunRequest,
} from '@runkora/shared/api';
import { indexAgentTypes, toAgentTypeRow, toRunRow } from '../_types';
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
    const body = (await res.json().catch(() => ({}))) as { message?: string };
    return { data: [], error: loadErrorMessage(res.status, 'agents', body.message) };
  }
  const body = (await res.json()) as { data: AgentType[] };
  return { data: (body.data ?? []).map(toAgentTypeRow), error: null };
}

// ── fetchPeriodRead ──
// A period-keyed BFF read: the Agents page rollups or one agent's overview. Never throws;
// on error `data` is null.

export async function fetchPeriodRead<T>(
  url: string,
): Promise<{ data: T | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(url, { cache: 'no-store' });
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
  return { data: (await res.json()) as T, error: null };
}

export const fetchAgentInsights = (period: AgentInsightsPeriod) =>
  fetchPeriodRead<AgentInsightsResponse>(ApiRoute.agentInsights.window(period));

// ── fetchRuns ─────────────────────────────────────────────────────────────────

/** A page of runs to read: filters, page size and the previous page's cursor. */
export interface RunPageQuery {
  states?: readonly RunState[];
  limit?: number;
  cursor?: string | null;
}

/** The BFF query string for a page of runs, with its leading `?` when not empty. */
export function runPageSearch(query: RunPageQuery): string {
  const params = new URLSearchParams();
  if (query.states?.length) params.set('state', query.states.join(','));
  if (query.limit) params.set('limit', String(query.limit));
  if (query.cursor) params.set('cursor', query.cursor);
  const qs = params.toString();
  return qs ? `?${qs}` : '';
}

export interface RunPageResult {
  data: RunRow[];
  /** The next page's cursor; null on the last page. */
  nextCursor: string | null;
  error: string | null;
}

/** Fetches one page of runs from a BFF run list route. Never throws. */
export async function fetchRunPage(
  url: string,
  agentTypes: AgentTypeRow[],
): Promise<RunPageResult> {
  let res: Response;
  try {
    res = await fetch(url, { cache: 'no-store' });
  } catch {
    return { data: [], nextCursor: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { message?: string };
    return {
      data: [],
      nextCursor: null,
      error: loadErrorMessage(res.status, 'runs', body.message),
    };
  }
  const body = (await res.json()) as RunsPageResponse;
  const byId = indexAgentTypes(agentTypes);
  return {
    data: (body.data ?? []).map((r) => toRunRow(r, byId)),
    nextCursor: body.next_cursor ?? null,
    error: null,
  };
}

/** One page of the runs of every agent the caller may read. */
export const fetchRuns = (agentTypes: AgentTypeRow[], query: RunPageQuery = {}) =>
  fetchRunPage(`${ApiRoute.runs.list()}${runPageSearch(query)}`, agentTypes);

// ── fetchRunStats ─────────────────────────────────────────────────────────────

export async function fetchRunStats(
  period: AgentInsightsPeriod,
): Promise<{ data: AgentRunStats | null; error: string | null }> {
  const result = await fetchPeriodRead<{ data: AgentRunStats | null }>(ApiRoute.runs.stats(period));
  return { data: result.data?.data ?? null, error: result.error };
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
  subject: string,
): Promise<{ data: T; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(url, { cache: 'no-store' });
  } catch {
    return { data: empty, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { message?: string };
    return { data: empty, error: loadErrorMessage(res.status, subject, body.message) };
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
        (m) => m.status === 'available' && (m.capabilities ?? []).includes('chat'),
      ),
    [],
    'models',
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
    'connections',
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
    'MCP tools',
  );
}

export function fetchEnvironments(): Promise<{ data: Environment[]; error: string | null }> {
  return fetchCatalogue(
    ApiRoute.environments.list(),
    (b) => (b as { data?: Environment[] }).data ?? [],
    [],
    'environments',
  );
}
