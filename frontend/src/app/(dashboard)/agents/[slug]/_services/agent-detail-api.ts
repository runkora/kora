// ── Agent detail — client-side BFF calls ──────────────────────────────────────

import { ApiRoute } from '@runkora/lib/routing/api-routes';
import type {
  AgentInsightsPeriod,
  AgentOverviewResponse,
  AgentType,
  EffectiveAccess,
  EffectiveAccessPreviewRequest,
  UpdateAgentTypeRequest,
} from '@runkora/shared/api';
import { toAgentTypeRow } from '../../_types';
import type { AgentTypeRow } from '../../_types';
import { fetchPeriodRead, fetchRunPage, runPageSearch } from '../../_services/agents-api';
import type { RunPageQuery } from '../../_services/agents-api';

// ── fetchAgentDetail ──────────────────────────────────────────────────────────

export async function fetchAgentDetail(
  id: string,
): Promise<{ agentType: AgentTypeRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.detail(id), { cache: 'no-store' });
  } catch {
    return { agentType: null, error: 'Network error: could not reach the server.' };
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

// ── fetchEffectiveAccess ──────────────────────────────────────────────────────

export async function fetchEffectiveAccess(
  id: string,
): Promise<{ access: EffectiveAccess | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.effectiveAccess(id), { cache: 'no-store' });
  } catch {
    return { access: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      access: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: EffectiveAccess };
  return { access: body.data, error: null };
}

// ── previewEffectiveAccess ────────────────────────────────────────────────────

// Resolves the effective-access view for a *draft* (unsaved) connectivity config
// — backs the Connectivity tab's live preview as the user edits.
export async function previewEffectiveAccess(
  body: EffectiveAccessPreviewRequest,
): Promise<{ access: EffectiveAccess | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.previewEffectiveAccess(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      cache: 'no-store',
    });
  } catch {
    return { access: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const errBody = await res.json().catch(() => ({}) as { message?: string });
    return {
      access: null,
      error: (errBody as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const okBody = (await res.json()) as { data: EffectiveAccess };
  return { access: okBody.data, error: null };
}

// ── deleteAgentType ───────────────────────────────────────────────────────────

export async function deleteAgentType(id: string): Promise<{ error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.detail(id), { method: 'DELETE' });
  } catch {
    return { error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return { error: (body as { message?: string }).message ?? `Request failed (${res.status})` };
  }
  return { error: null };
}

// ── fetchAgentRuns ────────────────────────────────────────────────────────────

/** One page of one agent's runs, newest first. Pass the previous page's cursor for the next. */
export const fetchAgentRuns = (agentTypeRow: AgentTypeRow, query: RunPageQuery = {}) =>
  fetchRunPage(`${ApiRoute.runs.listForType(agentTypeRow.id)}${runPageSearch(query)}`, [
    agentTypeRow,
  ]);

// ── deleteRun ─────────────────────────────────────────────────────────────────

export async function deleteRun(runId: string): Promise<{ error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.runs.detail(runId), { method: 'DELETE' });
  } catch {
    return { error: 'Network error: could not reach the server.' };
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

// ── fetchAgentOverview ────────────────────────────────────────────────────────

export const fetchAgentOverview = (slug: string, period: AgentInsightsPeriod) =>
  fetchPeriodRead<AgentOverviewResponse>(ApiRoute.agentTypes.overview(slug, period));
