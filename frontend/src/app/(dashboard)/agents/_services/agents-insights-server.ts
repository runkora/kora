import 'server-only';

import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { config } from '@orbit/server/config';
import { policyGatewayFetch } from '@orbit/server/services/policy-gateway-server';
import { hasScopeCovering } from '@orbit/lib/permissions';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import { PERIOD_TEXT } from '../_lib/spend';
import type { Period } from '../_lib/spend';

// The Agents page rollups. The inference gateway counts usage and budgets for
// the caller's own token, and orbit-agents counts runs on the run list rule, so
// the BFF only composes what each returns. It sends one end time to both so the
// two cut the same window.

type Result<T> = { ok: true; data: T } | { ok: false; status: number; message: string };

const UPSTREAM_TIMEOUT_MS = 10_000;

/** The default window, matching the gateway's. */
const DEFAULT_PERIOD: Period = '7d';

/** Reads a `period` query value. Absent means the default window; anything else unknown is null. */
export function parsePeriod(raw: string | null): Period | null {
  if (raw === null || raw === '') return DEFAULT_PERIOD;
  return Object.hasOwn(PERIOD_TEXT, raw) ? (raw as Period) : null;
}

async function gatewayGet<T>(path: string, accessToken: string): Promise<Result<T>> {
  let res: Response;
  try {
    res = await fetch(`${config.inferenceGatewayUrl}${path}`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
    });
  } catch {
    return { ok: false, status: 502, message: 'Failed to reach inference gateway' };
  }
  if (!res.ok) {
    return { ok: false, status: res.status, message: `Inference gateway responded ${res.status}` };
  }
  try {
    return { ok: true, data: (await res.json()) as T };
  } catch {
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }
}

async function agentsGet<T>(path: string, accessToken: string): Promise<Result<T>> {
  try {
    const data = await agentsClient(accessToken).get<T>(path);
    if (data === undefined) return { ok: false, status: 502, message: 'Empty response' };
    return { ok: true, data };
  } catch (err) {
    if (err instanceof ApiError) return { ok: false, status: err.status, message: err.message };
    return { ok: false, status: 502, message: 'Failed to reach orbit-agents' };
  }
}

/** The tenant cap, fetched only for a session that holds the scope that guards it. */
async function loadTenantCap(accessToken: string): Promise<AgentInsightsResponse['tenant_cap']> {
  const scopes = await policyGatewayFetch<{ scopes: string[] }>('/scopes', accessToken);
  if (!scopes.ok || !hasScopeCovering(scopes.data.scopes, 'platform:budgets:read'))
    return undefined;
  const cap = await gatewayGet<{ tenant_cap?: AgentInsightsResponse['tenant_cap'] }>(
    '/budgets/tenant-cap',
    accessToken,
  );
  return cap.ok ? cap.data.tenant_cap : undefined;
}

/**
 * Usage, model split, spend over time and budgets from the gateway, run
 * outcomes from orbit-agents and, for a caller who may read it, the tenant cap,
 * all at once. Outcomes and the cap are optional: when either cannot be had the
 * rest still stands.
 */
export async function loadInsights(
  accessToken: string,
  period: Period,
): Promise<Result<AgentInsightsResponse>> {
  const query = `period=${period}&end=${encodeURIComponent(new Date().toISOString())}`;
  const [usage, outcomes, tenantCap] = await Promise.all([
    gatewayGet<AgentInsightsResponse>(`/usage/agent-insights?${query}`, accessToken),
    agentsGet<{ completed: number; failed: number }>(`/api/v1/run-outcomes?${query}`, accessToken),
    loadTenantCap(accessToken),
  ]);
  if (!usage.ok) return usage;
  return {
    ok: true,
    data: {
      ...usage.data,
      ...(outcomes.ok ? { outcomes: outcomes.data } : {}),
      ...(tenantCap ? { tenant_cap: tenantCap } : {}),
    },
  };
}
