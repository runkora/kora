import 'server-only';

import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { config } from '@orbit/server/config';
import type { AgentBudgetsResponse, AgentInsightsResponse } from '@orbit/shared/api';
import { PERIOD_TEXT } from '../_lib/spend';
import type { Period } from '../_lib/spend';

// The Agents page rollups. The gateway counts usage for the agents the caller
// may see and orbit-agents counts runs and reads budget limits on the same
// terms, so the BFF only composes what each returns; it never filters.

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

/**
 * Usage, model split, spend over time and run outcomes for one window. The
 * gateway and orbit-agents are asked together. Run outcomes are optional: when
 * orbit-agents cannot count them the rest still stands.
 */
export async function loadInsights(
  accessToken: string,
  period: Period,
): Promise<Result<AgentInsightsResponse>> {
  const [usage, outcomes] = await Promise.all([
    gatewayGet<AgentInsightsResponse>(`/usage/agent-insights?period=${period}`, accessToken),
    agentsGet<{ completed: number; failed: number }>(
      `/api/v1/run-outcomes?period=${period}`,
      accessToken,
    ),
  ]);
  if (!usage.ok) return usage;
  return {
    ok: true,
    data: outcomes.ok ? { ...usage.data, outcomes: outcomes.data } : usage.data,
  };
}

interface GatewayBudgets {
  agents: { agent_slug: string; week_to_date_usd: number; month_to_date_usd: number }[];
}

interface GatewayTenantCap {
  tenant_cap?: { limit_usd: number; reset_cadence: string; spend_usd: number };
}

/**
 * Each visible agent's spend in its budget windows beside its limit, and the
 * tenant cap for callers who may read platform budgets. The cap comes from its
 * own gateway read, not the per-principal budget report. Spend and limits are
 * both needed: a row with only one would misstate the other. The tenant cap is
 * optional, and a 403 is the expected answer for most callers.
 */
export async function loadBudgets(accessToken: string): Promise<Result<AgentBudgetsResponse>> {
  const [spend, limits, cap] = await Promise.all([
    gatewayGet<GatewayBudgets>('/usage/agent-budgets', accessToken),
    agentsGet<{
      data: { agent_slug: string; limit_usd: number; reset_cadence: string }[];
    }>('/api/v1/agent-budgets', accessToken),
    gatewayGet<GatewayTenantCap>('/budgets/tenant-cap', accessToken),
  ]);
  if (!spend.ok) return spend;
  if (!limits.ok) return limits;
  if (!cap.ok && cap.status !== 403) {
    console.error(`[loadBudgets] tenant budget unavailable (${cap.status})`);
  }

  const limitBySlug = new Map(limits.data.data.map((l) => [l.agent_slug, l]));
  const spendBySlug = new Map(spend.data.agents.map((a) => [a.agent_slug, a]));
  const slugs = [...new Set([...spendBySlug.keys(), ...limitBySlug.keys()])];

  const agents = slugs.map((slug) => {
    const s = spendBySlug.get(slug);
    const l = limitBySlug.get(slug);
    return {
      agent_slug: slug,
      week_to_date_usd: s?.week_to_date_usd ?? 0,
      month_to_date_usd: s?.month_to_date_usd ?? 0,
      ...(l ? { limit_usd: l.limit_usd, reset_cadence: l.reset_cadence } : {}),
    };
  });
  const tenantCap = cap.ok ? cap.data.tenant_cap : undefined;
  return { ok: true, data: { agents, ...(tenantCap ? { tenant_cap: tenantCap } : {}) } };
}
