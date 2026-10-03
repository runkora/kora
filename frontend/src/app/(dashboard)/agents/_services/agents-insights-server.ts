import 'server-only';

import { config } from '@orbit/server/config';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import { PERIOD_TEXT } from '../_lib/spend';
import type { Period } from '../_lib/spend';

// The Agents page rollups. The inference gateway counts usage and budgets under
// the caller's own token, and orbit-agents counts runs on the run list rule, so
// the BFF only composes what each returns.

type Result<T> = { ok: true; data: T } | { ok: false; status: number };

/** Reads a `period` query value. Absent means 7d; anything unknown is null. */
export function parsePeriod(raw: string | null): Period | null {
  if (raw === null || raw === '') return '7d';
  return Object.hasOwn(PERIOD_TEXT, raw) ? (raw as Period) : null;
}

/** GET from an upstream service with the caller's token. */
async function get<T>(base: string, path: string, accessToken: string): Promise<Result<T>> {
  try {
    const res = await fetch(`${base}${path}`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
    return res.ok ? { ok: true, data: (await res.json()) as T } : { ok: false, status: res.status };
  } catch {
    return { ok: false, status: 502 };
  }
}

/**
 * Usage, model split, spend over time and budgets from the gateway, run
 * outcomes from orbit-agents and the tenant cap, all at once. Outcomes and the
 * cap are optional: a caller without platform:budgets:read gets a 403 for the
 * cap, which reads as no cap.
 */
export async function loadInsights(
  accessToken: string,
  period: Period,
): Promise<Result<AgentInsightsResponse>> {
  const [usage, outcomes, cap] = await Promise.all([
    get<AgentInsightsResponse>(
      config.inferenceGatewayUrl,
      `/usage/agent-insights?period=${period}`,
      accessToken,
    ),
    get<{ completed: number; failed: number }>(
      config.orbitAgentsUrl,
      `/api/v1/run-outcomes?period=${period}`,
      accessToken,
    ),
    get<{ tenant_cap?: AgentInsightsResponse['tenant_cap'] }>(
      config.inferenceGatewayUrl,
      '/budgets/tenant-cap',
      accessToken,
    ),
  ]);
  if (!usage.ok) return usage;
  return {
    ok: true,
    data: {
      ...usage.data,
      ...(outcomes.ok ? { outcomes: outcomes.data } : {}),
      ...(cap.ok && cap.data.tenant_cap ? { tenant_cap: cap.data.tenant_cap } : {}),
    },
  };
}
