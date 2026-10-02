/**
 * Mock kora workspace for the dashboard home stories. Agents, runs, connections
 * and environments reuse the Agents page fixtures so the two pages tell the same
 * story; analytics and the tenant budget are authored here.
 */

import {
  AGENT_ROWS,
  AGENT_TYPES,
  CONNECTIONS,
  ENVIRONMENTS,
  MANY_AGENT_ROWS,
  MANY_RUN_ROWS,
  RUN_ROWS,
} from '../agents/_components/agents-mock-data';
import { buildConnectionRows } from '../connections/_types';
import { toEnvironmentRow } from '../environments/_types';
import type { AnalyticsBucket, AnalyticsResponse, TenantCapStatus } from '../model-gateway/_types';
import type { DashboardPageProps } from './dashboard-page';

const DAY = 24 * 60 * 60 * 1000;

/** Calendar date (YYYY-MM-DD) for a day `daysAgo` before today. */
const dateAgo = (daysAgo: number): string =>
  new Date(Date.now() - daysAgo * DAY).toISOString().slice(0, 10);

/** One daily bucket derived from a request count and a failure rate, so the numbers stay consistent. */
function bucket(daysAgo: number, requests: number, failureRate: number): AnalyticsBucket {
  const failed = Math.round(requests * failureRate);
  return {
    date: dateAgo(daysAgo),
    requests,
    successful_requests: requests - failed,
    failed_requests: failed,
    prompt_tokens: requests * 5_400,
    completion_tokens: requests * 1_150,
    cache_read_tokens: requests * 3_200,
    cache_creation_tokens: requests * 420,
    estimated_cost_usd: Math.round(requests * 0.071 * 100) / 100,
    p50_latency_ms: 1_450,
    p95_latency_ms: 4_300,
    p99_latency_ms: 7_900,
  };
}

function analytics(
  daily: number[],
  failureRate: number,
  overrides: Partial<AnalyticsResponse['kpis']> = {},
): AnalyticsResponse {
  const buckets = daily.map((requests, i) => bucket(daily.length - 1 - i, requests, failureRate));
  const total = daily.reduce((sum, n) => sum + n, 0);
  const cost = buckets.reduce((sum, b) => sum + b.estimated_cost_usd, 0);
  return {
    scope: 'tenant',
    period_days: 7,
    granularity: 'daily',
    kpis: {
      total_requests: total,
      total_tokens: total * 6_550,
      prompt_tokens: total * 5_400,
      completion_tokens: total * 1_150,
      cache_read_tokens: total * 3_200,
      cache_creation_tokens: total * 420,
      estimated_cost_usd: Math.round(cost * 100) / 100,
      avg_latency_ms: 1_820,
      p95_latency_ms: 4_300,
      p99_latency_ms: 7_900,
      error_rate: failureRate,
      cache_hit_rate: 0.59,
      ...overrides,
    },
    buckets,
    cost_by_model: [
      { model_id: 'anthropic/claude-sonnet-4-6', estimated_cost_usd: Math.round(cost * 0.72) },
      { model_id: 'anthropic/claude-haiku-4-5', estimated_cost_usd: Math.round(cost * 0.28) },
    ],
    tokens_by_model: [],
  };
}

const CONNECTION_ROWS = buildConnectionRows(CONNECTIONS, AGENT_TYPES);
const ENVIRONMENT_ROWS = ENVIRONMENTS.map(toEnvironmentRow);

/** A busy workspace: many agents and runs, steady traffic, budget comfortably within its cap. */
export const BUSY_WORKSPACE: DashboardPageProps = {
  agentTypes: MANY_AGENT_ROWS,
  runs: MANY_RUN_ROWS,
  connections: CONNECTION_ROWS,
  environments: ENVIRONMENT_ROWS,
  analytics: analytics([312, 428, 391, 506, 470, 214, 538], 0.012),
  tenantCap: { limit_usd: 1_500, reset_cadence: 'monthly', spend_usd: 612.4, reached: false },
  windowDays: 7,
};

/** A workspace that has just been created: nothing registered, no traffic, no budget. */
export const NEW_WORKSPACE: DashboardPageProps = {
  agentTypes: [],
  runs: [],
  connections: [],
  environments: [],
  analytics: null,
  tenantCap: null,
  windowDays: 7,
};

/** Spend near the cap, a failing run streak and an elevated error rate. */
export const ALERTING_WORKSPACE: DashboardPageProps = {
  agentTypes: AGENT_ROWS,
  runs: RUN_ROWS.map((run, i) =>
    i % 2 === 0 && run.state === 'completed' ? { ...run, state: 'failed' } : run,
  ),
  connections: CONNECTION_ROWS,
  environments: ENVIRONMENT_ROWS,
  analytics: analytics([402, 455, 489, 530, 517, 561, 603], 0.094, {
    p95_latency_ms: 9_800,
    p99_latency_ms: 18_400,
  }),
  tenantCap: { limit_usd: 800, reset_cadence: 'monthly', spend_usd: 742.15, reached: false },
  windowDays: 7,
};

/** The cap is exhausted, so the budget card flips to its limit-reached state. */
export const LIMIT_REACHED_WORKSPACE: DashboardPageProps = {
  ...ALERTING_WORKSPACE,
  tenantCap: {
    limit_usd: 800,
    reset_cadence: 'monthly',
    spend_usd: 800,
    reached: true,
  } satisfies TenantCapStatus,
};

/** A small team with light, quiet traffic. */
export const LIGHT_TRAFFIC: DashboardPageProps = {
  agentTypes: AGENT_ROWS.slice(0, 2),
  runs: RUN_ROWS.slice(0, 3),
  connections: CONNECTION_ROWS.slice(0, 1),
  environments: ENVIRONMENT_ROWS.slice(0, 1),
  analytics: analytics([0, 3, 12, 0, 8, 2, 15], 0),
  tenantCap: { limit_usd: 200, reset_cadence: 'monthly', spend_usd: 4.8, reached: false },
  windowDays: 7,
};
