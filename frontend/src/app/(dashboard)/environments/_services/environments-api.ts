// ── Environments API — client-side BFF calls ──────────────────────────────────
//
// All functions call the BFF routes under /api/agents/environments. Never
// throws — callers pattern-match on the returned `error` field.

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type {
  Connector,
  EffectiveAccess,
  EffectiveAccessPreviewRequest,
  Environment,
  EnvironmentRequest,
} from '@orbit/shared/api';
import { toEnvironmentRow } from '../_types';
import type { EnvironmentRow } from '../_types';

// ── fetchEnvironments ─────────────────────────────────────────────────────────

export async function fetchEnvironments(): Promise<{
  data: EnvironmentRow[];
  error: string | null;
}> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.environments.list(), { cache: 'no-store' });
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
  const body = (await res.json()) as { data: Environment[] };
  return { data: (body.data ?? []).map(toEnvironmentRow), error: null };
}

// ── createEnvironment ─────────────────────────────────────────────────────────

export async function createEnvironment(
  payload: EnvironmentRequest,
): Promise<{ environment: EnvironmentRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.environments.create(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { environment: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      environment: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: Environment };
  return { environment: toEnvironmentRow(body.data), error: null };
}

// ── updateEnvironment ─────────────────────────────────────────────────────────

export async function updateEnvironment(
  id: string,
  payload: EnvironmentRequest,
): Promise<{ environment: EnvironmentRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.environments.detail(id), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { environment: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      environment: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: Environment };
  return { environment: toEnvironmentRow(body.data), error: null };
}

// ── previewEnvironmentAccess ──────────────────────────────────────────────────

// Resolves the effective-access view for a *draft* environment. An environment
// is exactly a posture + connectors bundle, so the agent-types preview endpoint
// answers truthfully with no environment folded in (`environment_id: null`).
export async function previewEnvironmentAccess(body: {
  egressHosts: string[];
  connectors: Connector[];
}): Promise<{ access: EffectiveAccess | null; error: string | null }> {
  const request: EffectiveAccessPreviewRequest = {
    connectivity: { mode: 'whitelist', hosts: body.egressHosts },
    connectors: body.connectors,
    environment_id: null,
  };
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.previewEffectiveAccess(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
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

// ── deleteEnvironment ─────────────────────────────────────────────────────────

export async function deleteEnvironment(id: string): Promise<{ error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.environments.delete(id), { method: 'DELETE' });
  } catch {
    return { error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return { error: (body as { message?: string }).message ?? `Request failed (${res.status})` };
  }
  return { error: null };
}
