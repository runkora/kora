// ── Agent detail — client-side BFF calls ──────────────────────────────────────

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { AgentType, Run, UpdateAgentTypeRequest } from '@orbit/shared/api';
import { toAgentTypeRow, toRunRow } from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';

// ── fetchAgentDetail ──────────────────────────────────────────────────────────

export async function fetchAgentDetail(
  id: string,
): Promise<{ agentType: AgentTypeRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.detail(id), { cache: 'no-store' });
  } catch {
    return { agentType: null, error: 'Network error — could not reach the server.' };
  }
  if (res.status === 404) return { agentType: null, error: null };
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

// ── deleteAgentType ───────────────────────────────────────────────────────────

export async function deleteAgentType(id: string): Promise<{ error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.detail(id), { method: 'DELETE' });
  } catch {
    return { error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return { error: (body as { message?: string }).message ?? `Request failed (${res.status})` };
  }
  return { error: null };
}

// ── fetchAgentRuns ────────────────────────────────────────────────────────────

export async function fetchAgentRuns(
  agentTypeId: string,
  agentTypeRow: AgentTypeRow,
): Promise<{ data: RunRow[]; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.runs.listForType(agentTypeId), { cache: 'no-store' });
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
  return { data: (body.data ?? []).map((r) => toRunRow(r, [agentTypeRow])), error: null };
}

// ── deleteRun ─────────────────────────────────────────────────────────────────

export async function deleteRun(runId: string): Promise<{ error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.runs.detail(runId), { method: 'DELETE' });
  } catch {
    return { error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return { error: (body as { message?: string }).message ?? `Request failed (${res.status})` };
  }
  return { error: null };
}

// ── updateAgentType ───────────────────────────────────────────────────────────

export async function updateAgentType(
  id: string,
  payload: UpdateAgentTypeRequest,
): Promise<{ agentType: AgentTypeRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.detail(id), {
      method: 'PATCH',
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
