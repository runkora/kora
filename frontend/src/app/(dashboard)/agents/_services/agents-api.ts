// ── Agents API — client-side BFF calls ───────────────────────────────────────
//
// All functions call the BFF routes added in Plan 2. Never throws — callers
// pattern-match on the returned `error` field.

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type {
  AgentType,
  AgentTypeUsage,
  Run,
  CreateAgentTypeRequest,
  StartRunRequest,
} from '@orbit/shared/api';
import { indexAgentTypes, toAgentTypeRow, toRunRow } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

// ── fetchAgentTypes ───────────────────────────────────────────────────────────

export async function fetchAgentTypes(): Promise<{ data: AgentTypeRow[]; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.list(), { cache: 'no-store' });
  } catch {
    return { data: [], error: 'Network error — could not reach the server.' };
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
    return { byAgentType: {}, error: 'Network error — could not reach the server.' };
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

// ── fetchRuns ─────────────────────────────────────────────────────────────────

export async function fetchRuns(
  agentTypes: AgentTypeRow[],
): Promise<{ data: RunRow[]; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.runs.list(), { cache: 'no-store' });
  } catch {
    return { data: [], error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      data: [],
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: Run[] };
  const byId = indexAgentTypes(agentTypes);
  return { data: (body.data ?? []).map((r) => toRunRow(r, byId)), error: null };
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
    return { run: null, error: 'Network error — could not reach the server.' };
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
    return { agentType: null, error: 'Network error — could not reach the server.' };
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
