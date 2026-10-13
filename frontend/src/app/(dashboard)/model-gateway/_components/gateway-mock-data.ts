/**
 * Mock data for the API keys, Logs, My usage, Analytics and Budgets page
 * stories. It extends model-mock-data.ts with the keys, request logs, personal
 * analytics and budget payloads those pages render. Timestamps are relative to
 * load time so relative copy stays realistic whenever a story is opened.
 */

import type { CreatePATResponse, PAT } from '@orbit/shared/api';
import type { Grant, RoleEntry } from '../../policy/_types/api';
import type { TokenUsageStat } from '../_services/usage-server';
import type {
  AnalyticsBucket,
  AnalyticsDays,
  AnalyticsKPIs,
  AnalyticsModelTokenBucket,
  AnalyticsPrincipalsPageResponse,
  AnalyticsResponse,
  BudgetPrincipalRow,
  BudgetPrincipalsView,
  BudgetStatus,
  InferenceUsageRecord,
  InferenceUsageStatus,
  PrincipalIdentity,
  PrincipalUsageRow,
} from '../_types';
import { COST_BY_MODEL } from './model-mock-data';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

const ago = (ms: number): string => new Date(Date.now() - ms).toISOString();
const fromNow = (ms: number): string => new Date(Date.now() + ms).toISOString();

const TENANT = 'ten_8f3a21';

// ── API keys ─────────────────────────────────────────────────────────────────

export const KEY_IDS = {
  codeReviewer: '3b1f6c52-9a4e-4d0b-8f27-1c5e7a90d314',
  coder: '7d2e90a1-58c3-4b6f-a1d4-0e9b3f6c2a58',
  security: 'c94a0d73-2e1b-47f8-9b36-5a8d1e4f7c20',
  evalHarness: '5e8c3b17-d6a2-4f90-b4e1-72a09c6d8f13',
  retired: 'a07f5d29-4c83-4e1a-8d52-9b6e0c3a71f4',
  expired: 'f2c61e84-0b97-4a35-9d18-3e7a5b0c4d69',
} as const;

const INFERENCE_SCOPES = ['inference:chat', 'inference:embeddings'];

/** Every scope the signed-in operator may delegate to a new key. */
export const EFFECTIVE_SCOPES = [
  'inference:chat',
  'inference:embeddings',
  'inference:models:read',
  'agents:read',
  'agents:write',
  'agents:runs:read',
  'agents:runs:write',
  'mcp:github:read',
  'mcp:github:write',
  'mcp:linear:read',
  'connection:github',
  'egress:api.github.com',
];

export const PATS: PAT[] = [
  {
    id: KEY_IDS.codeReviewer,
    name: 'code-reviewer-ci',
    scopes: [...INFERENCE_SCOPES, 'inference:models:read'],
    created_at: ago(41 * DAY),
    expires_at: fromNow(49 * DAY),
    revoked_at: null,
  },
  {
    id: KEY_IDS.coder,
    name: 'coder-local-dev',
    scopes: ['inference:chat', 'agents:runs:read', 'agents:runs:write', 'mcp:github:read'],
    created_at: ago(12 * DAY),
    expires_at: fromNow(78 * DAY),
    revoked_at: null,
  },
  {
    id: KEY_IDS.security,
    name: 'security-reviewer-nightly',
    scopes: INFERENCE_SCOPES,
    created_at: ago(63 * DAY),
    expires_at: fromNow(27 * DAY),
    revoked_at: null,
  },
  {
    id: KEY_IDS.evalHarness,
    name: 'eval-harness',
    scopes: ['inference:chat'],
    created_at: ago(5 * DAY),
    expires_at: fromNow(360 * DAY),
    revoked_at: null,
  },
  {
    id: KEY_IDS.retired,
    name: 'coder-prototype',
    scopes: ['inference:chat', 'agents:runs:write'],
    created_at: ago(88 * DAY),
    expires_at: fromNow(2 * DAY),
    revoked_at: ago(19 * DAY),
  },
  {
    id: KEY_IDS.expired,
    name: 'onboarding-demo',
    scopes: ['inference:chat'],
    created_at: ago(130 * DAY),
    expires_at: ago(10 * DAY),
    revoked_at: null,
  },
];

export const TOKEN_USAGE_STATS: TokenUsageStat[] = [
  { token_id: KEY_IDS.codeReviewer, request_count: 48_213, last_used: ago(3 * MINUTE) },
  { token_id: KEY_IDS.coder, request_count: 9_840, last_used: ago(2 * HOUR) },
  { token_id: KEY_IDS.security, request_count: 1_302, last_used: ago(9 * HOUR) },
  { token_id: KEY_IDS.evalHarness, request_count: 214, last_used: ago(1 * DAY) },
  { token_id: KEY_IDS.retired, request_count: 5_771, last_used: ago(19 * DAY) },
];

/** What the create flow hands back once, shown in the "API key created" modal. */
export const CREATED_PAT: CreatePATResponse = {
  id: '0e6b4a92-7c15-4d38-a2f9-61d3b8e05c47',
  name: 'code-reviewer-staging',
  token:
    'orb_eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c3JfNDJiN2U5Iiwic2NwIjpbImluZmVyZW5jZTpjaGF0Il19.Zk3vQe1xPq9TnW5mB7rYcL2dHsA8uJf0oGiXaNtVb4E',
  scopes: ['inference:chat', 'inference:embeddings'],
  expires_at: fromNow(90 * DAY),
  created_at: new Date().toISOString(),
};

// ── Request logs ─────────────────────────────────────────────────────────────

interface LogSeed {
  model: string;
  provider: string;
  dialect: string;
  token: string | null;
  principal: { id: string; type: string };
  status?: InferenceUsageStatus;
  prompt: number;
  completion: number;
  cacheRead?: number;
  cacheWrite?: number;
  latency: number;
  minutesAgo: number;
}

const AGENT_CODE_REVIEWER = {
  id: 'spiffe://kora.example/ns/tenant-8f3a21/agent/code-reviewer',
  type: 'agent',
};
const AGENT_CODER = { id: 'spiffe://kora.example/ns/tenant-8f3a21/agent/coder', type: 'agent' };
const AGENT_SECURITY = {
  id: 'spiffe://kora.example/ns/tenant-8f3a21/agent/security-reviewer',
  type: 'agent',
};
const HUMAN = { id: 'usr_42b7e9', type: 'human' };

const COST_PER_MILLION: Record<string, [number, number]> = {
  'anthropic/claude-sonnet-4-6': [3, 15],
  'anthropic/claude-haiku-4-5': [1, 5],
  'anthropic/claude-opus-4-1': [15, 75],
  'azure/gpt-5': [1.25, 10],
  'vertexai/gemini-2-5-flash': [0.3, 2.5],
};

function failureFor(status: InferenceUsageStatus): InferenceUsageRecord['failure_details'] {
  if (status === 'provider_error') {
    return {
      provider_http_status: 529,
      provider_error_type: 'overloaded_error',
      provider_error_message: 'Overloaded. The model is receiving more requests than it can serve.',
    };
  }
  return null;
}

const ERROR_CODES: Partial<Record<InferenceUsageStatus, string>> = {
  provider_error: 'upstream_overloaded',
  policy_denied: 'scope_missing',
  auth_error: 'token_revoked',
  timeout: 'upstream_timeout',
  routing_failed: 'no_route_available',
  canceled: 'client_disconnected',
};

function record(n: number, seed: LogSeed): InferenceUsageRecord {
  const status = seed.status ?? 'success';
  const ok = status === 'success';
  const [inRate, outRate] = COST_PER_MILLION[seed.model] ?? [3, 15];
  const cacheRead = seed.cacheRead ?? 0;
  const cost = ok
    ? ((seed.prompt - cacheRead) * inRate + cacheRead * inRate * 0.1 + seed.completion * outRate) /
      1_000_000
    : 0;
  const [family, name] = seed.model.split('/');
  return {
    id: `req_${(0x9a31c0 + n * 7919).toString(16)}${n.toString(36).padStart(3, '0')}`,
    tenant_id: TENANT,
    principal_id: seed.principal.id,
    principal_type: seed.principal.type,
    token_id: seed.token,
    api_dialect: seed.dialect,
    provider: seed.provider,
    virtual_model_id: seed.model,
    model_id: seed.provider === 'bedrock' ? `us.anthropic.${name}-v1:0` : (name ?? seed.model),
    model_family: family ?? null,
    routing_strategy: 'priority',
    prompt_tokens: ok ? seed.prompt : seed.prompt,
    completion_tokens: ok ? seed.completion : 0,
    total_tokens: ok ? seed.prompt + seed.completion : seed.prompt,
    cache_read_input_tokens: cacheRead,
    cache_creation_input_tokens: seed.cacheWrite ?? 0,
    estimated_cost_usd: Math.round(cost * 10_000) / 10_000,
    status,
    retry_count: status === 'provider_error' ? 2 : 0,
    error_code: ok ? null : (ERROR_CODES[status] ?? status),
    failure_details: failureFor(status),
    latency_ms: seed.latency,
    created_at: ago(seed.minutesAgo * MINUTE),
  };
}

const LOG_SEEDS: LogSeed[] = [
  {
    model: 'anthropic/claude-sonnet-4-6',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.codeReviewer,
    principal: AGENT_CODE_REVIEWER,
    prompt: 18_400,
    completion: 920,
    cacheRead: 15_200,
    latency: 2140,
    minutesAgo: 3,
  },
  {
    model: 'anthropic/claude-haiku-4-5',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.coder,
    principal: AGENT_CODER,
    prompt: 4_200,
    completion: 310,
    latency: 640,
    minutesAgo: 7,
  },
  {
    model: 'azure/gpt-5',
    provider: 'azure',
    dialect: 'openai',
    token: KEY_IDS.evalHarness,
    principal: HUMAN,
    status: 'provider_error',
    prompt: 12_900,
    completion: 0,
    latency: 4820,
    minutesAgo: 11,
  },
  {
    model: 'anthropic/claude-sonnet-4-6',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.codeReviewer,
    principal: AGENT_CODE_REVIEWER,
    prompt: 22_700,
    completion: 1_480,
    cacheRead: 19_800,
    cacheWrite: 1_100,
    latency: 2890,
    minutesAgo: 14,
  },
  {
    model: 'anthropic/claude-opus-4-1',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.security,
    principal: AGENT_SECURITY,
    prompt: 41_300,
    completion: 3_120,
    cacheRead: 30_000,
    latency: 7310,
    minutesAgo: 22,
  },
  {
    model: 'anthropic/claude-sonnet-4-6',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.retired,
    principal: AGENT_CODER,
    status: 'auth_error',
    prompt: 0,
    completion: 0,
    latency: 38,
    minutesAgo: 31,
  },
  {
    model: 'vertexai/gemini-2-5-flash',
    provider: 'vertexai',
    dialect: 'openai',
    token: KEY_IDS.evalHarness,
    principal: HUMAN,
    prompt: 6_800,
    completion: 540,
    latency: 980,
    minutesAgo: 38,
  },
  {
    model: 'anthropic/claude-sonnet-4-6',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.coder,
    principal: AGENT_CODER,
    status: 'policy_denied',
    prompt: 0,
    completion: 0,
    latency: 21,
    minutesAgo: 44,
  },
  {
    model: 'anthropic/claude-sonnet-4-6',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.codeReviewer,
    principal: AGENT_CODE_REVIEWER,
    prompt: 16_100,
    completion: 760,
    cacheRead: 14_900,
    latency: 1890,
    minutesAgo: 52,
  },
  {
    model: 'anthropic/claude-opus-4-1',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.security,
    principal: AGENT_SECURITY,
    status: 'timeout',
    prompt: 58_000,
    completion: 0,
    latency: 60_000,
    minutesAgo: 67,
  },
  {
    model: 'anthropic/claude-haiku-4-5',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.coder,
    principal: AGENT_CODER,
    prompt: 3_700,
    completion: 260,
    latency: 570,
    minutesAgo: 74,
  },
  {
    model: 'azure/gpt-5',
    provider: 'azure',
    dialect: 'openai',
    token: null,
    principal: HUMAN,
    prompt: 9_400,
    completion: 1_200,
    latency: 3340,
    minutesAgo: 91,
  },
  {
    model: 'anthropic/claude-sonnet-4-6',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.codeReviewer,
    principal: AGENT_CODE_REVIEWER,
    status: 'canceled',
    prompt: 19_000,
    completion: 0,
    latency: 1210,
    minutesAgo: 108,
  },
  {
    model: 'anthropic/claude-sonnet-4-6',
    provider: 'bedrock',
    dialect: 'anthropic',
    token: KEY_IDS.coder,
    principal: AGENT_CODER,
    prompt: 27_600,
    completion: 2_050,
    cacheRead: 22_000,
    latency: 3410,
    minutesAgo: 126,
  },
  {
    model: 'vertexai/gemini-2-5-flash',
    provider: 'vertexai',
    dialect: 'openai',
    token: KEY_IDS.evalHarness,
    principal: HUMAN,
    status: 'routing_failed',
    prompt: 0,
    completion: 0,
    latency: 14,
    minutesAgo: 141,
  },
];

export const LOG_RECORDS: InferenceUsageRecord[] = LOG_SEEDS.map((seed, i) => record(i + 1, seed));

/** Fifty rows of mixed traffic, enough to exercise paging and the table scroll. */
export const MANY_LOG_RECORDS: InferenceUsageRecord[] = Array.from({ length: 50 }, (_, i) => {
  const base = LOG_SEEDS[i % LOG_SEEDS.length] as LogSeed;
  return record(100 + i, {
    ...base,
    latency: base.latency + ((i * 131) % 700),
    minutesAgo: 3 + i * 17,
  });
});

/** One record per status, for the detail-modal and filtered stories. */
export const FAILED_LOG_RECORD: InferenceUsageRecord = LOG_RECORDS[2] as InferenceUsageRecord;

// ── Personal and tenant analytics ────────────────────────────────────────────

const ANALYTICS_MODELS = COST_BY_MODEL.map((c) => c.model_id);

/** Share of cost each model carries, from the Overview mock, so charts agree across pages. */
const MODEL_SHARE = (() => {
  const total = COST_BY_MODEL.reduce((sum, c) => sum + c.estimated_cost_usd, 0);
  return COST_BY_MODEL.map((c) => c.estimated_cost_usd / total);
})();

function isoDay(daysBack: number): string {
  return new Date(Date.now() - daysBack * DAY).toISOString().slice(0, 10);
}

function buckets(days: number, requestsPerDay: number): AnalyticsBucket[] {
  return Array.from({ length: days }, (_, i) => {
    const back = days - i;
    const dow = new Date(Date.now() - back * DAY).getUTCDay();
    const weekend = dow === 0 || dow === 6;
    const requests = Math.round((weekend ? 0.4 : 1) * requestsPerDay + ((i * 937) % 140));
    const failed = Math.round(requests * (0.012 + ((i * 7) % 5) / 1000));
    const prompt = requests * 1450;
    return {
      date: isoDay(back),
      requests,
      successful_requests: requests - failed,
      failed_requests: failed,
      prompt_tokens: prompt,
      completion_tokens: Math.round(prompt * 0.22),
      cache_read_tokens: Math.round(prompt * 0.6),
      cache_creation_tokens: Math.round(prompt * 0.05),
      estimated_cost_usd: Math.round(requests * 0.0094 * 100) / 100,
      p50_latency_ms: 880 + ((i * 53) % 140),
      p95_latency_ms: 3100 + ((i * 97) % 600),
      p99_latency_ms: 6400 + ((i * 131) % 900),
    };
  });
}

function tokensByModel(bs: AnalyticsBucket[]): AnalyticsModelTokenBucket[] {
  return bs.flatMap((b) =>
    ANALYTICS_MODELS.map((model_id, i) => ({
      date: b.date,
      model_id,
      total_tokens: Math.round(
        (b.prompt_tokens + b.completion_tokens) * (MODEL_SHARE[i] ?? 0) * (1 + ((i * 3) % 5) / 40),
      ),
    })),
  );
}

function kpisFor(bs: AnalyticsBucket[]): AnalyticsKPIs {
  const sum = (key: keyof AnalyticsBucket) => bs.reduce((total, b) => total + Number(b[key]), 0);
  const requests = sum('requests');
  return {
    total_requests: requests,
    total_tokens: sum('prompt_tokens') + sum('completion_tokens'),
    prompt_tokens: sum('prompt_tokens'),
    completion_tokens: sum('completion_tokens'),
    cache_read_tokens: sum('cache_read_tokens'),
    cache_creation_tokens: sum('cache_creation_tokens'),
    estimated_cost_usd: sum('estimated_cost_usd'),
    avg_latency_ms: requests > 0 ? 1120 : 0,
    p95_latency_ms: requests > 0 ? 3380 : 0,
    p99_latency_ms: requests > 0 ? 6900 : 0,
    error_rate: requests > 0 ? sum('failed_requests') / requests : 0,
    cache_hit_rate: requests > 0 ? 0.58 : 0,
  };
}

/** Builds the analytics payload the BFF returns for a window of `days`. */
export function analyticsResponse(
  scope: AnalyticsResponse['scope'],
  days: AnalyticsDays,
  requestsPerDay: number,
): AnalyticsResponse {
  const bs = buckets(days, requestsPerDay);
  const kpis = kpisFor(bs);
  const spend = kpis.estimated_cost_usd;
  return {
    scope,
    period_days: days,
    granularity: 'daily',
    kpis,
    buckets: bs,
    cost_by_model: COST_BY_MODEL.map((c, i) => ({
      model_id: c.model_id,
      estimated_cost_usd: Math.round(spend * (MODEL_SHARE[i] ?? 0) * 100) / 100,
    })),
    tokens_by_model: tokensByModel(bs),
  };
}

/** A window with no traffic: zeroed KPIs and no buckets. */
export function emptyAnalytics(
  scope: AnalyticsResponse['scope'],
  days: AnalyticsDays,
): AnalyticsResponse {
  return {
    scope,
    period_days: days,
    granularity: 'daily',
    kpis: kpisFor([]),
    buckets: [],
    cost_by_model: [],
    tokens_by_model: [],
  };
}

export const MY_USAGE_7D = analyticsResponse('personal', 7, 380);
export const TENANT_ANALYTICS_7D = analyticsResponse('tenant', 7, 12_800);

// ── Per-principal analytics ──────────────────────────────────────────────────

function principalRow(
  seed: Pick<PrincipalUsageRow, 'principal_id' | 'principal_type' | 'email' | 'display_name'>,
  requests: number,
  costUsd: number,
  errorRate: number,
  lastActiveMinutes: number,
): PrincipalUsageRow {
  const prompt = requests * 1450;
  const completion = Math.round(prompt * 0.22);
  return {
    ...seed,
    request_count: requests,
    total_tokens: prompt + completion,
    prompt_tokens: prompt,
    completion_tokens: completion,
    cache_read_tokens: Math.round(prompt * 0.6),
    cache_creation_tokens: Math.round(prompt * 0.05),
    estimated_cost_usd: costUsd,
    error_rate: errorRate,
    avg_latency_ms: 900 + Math.round(errorRate * 40_000),
    last_active_at: ago(lastActiveMinutes * MINUTE),
  };
}

export const PRINCIPAL_ROWS: PrincipalUsageRow[] = [
  principalRow(
    {
      principal_id: AGENT_CODE_REVIEWER.id,
      principal_type: 'agent',
      email: null,
      display_name: null,
    },
    186_400,
    1_284.4,
    0.009,
    3,
  ),
  principalRow(
    { principal_id: AGENT_CODER.id, principal_type: 'agent', email: null, display_name: null },
    94_210,
    902.15,
    0.021,
    7,
  ),
  principalRow(
    { principal_id: AGENT_SECURITY.id, principal_type: 'agent', email: null, display_name: null },
    21_880,
    611.9,
    0.034,
    22,
  ),
  principalRow(
    {
      principal_id: HUMAN.id,
      principal_type: 'human',
      email: 'dev@kora.example',
      display_name: 'Dev Operator',
    },
    3_120,
    74.3,
    0.004,
    38,
  ),
  principalRow(
    {
      principal_id: 'usr_9c41d2',
      principal_type: 'human',
      email: 'priya.nair@kora.example',
      display_name: 'Priya Nair',
    },
    1_740,
    41.85,
    0.012,
    190,
  ),
];

export const PRINCIPALS_PAGE: AnalyticsPrincipalsPageResponse = {
  data: PRINCIPAL_ROWS,
  total: PRINCIPAL_ROWS.length,
  page: 1,
  limit: 25,
};

export const EMPTY_PRINCIPALS_PAGE: AnalyticsPrincipalsPageResponse = {
  data: [],
  total: 0,
  page: 1,
  limit: 25,
};

// ── Budgets ──────────────────────────────────────────────────────────────────

/** A personal budget with a weekly principal ceiling and a shared team pool. */
export const BUDGET_STATUS: BudgetStatus = {
  monthly_budget_usd: 400,
  current_spend_usd: 286.4,
  spend_limit_reached: false,
  has_limit: true,
  ceilings: [
    {
      scope: 'principal',
      id: HUMAN.id,
      window: 'monthly',
      limit_usd: 400,
      spend_usd: 286.4,
      reached: false,
    },
    {
      scope: 'role_pool',
      id: 'engineering',
      window: 'weekly',
      limit_usd: 2_500,
      spend_usd: 2_140.25,
      reached: false,
    },
  ],
};

export const BUDGET_STATUS_REACHED: BudgetStatus = {
  monthly_budget_usd: 400,
  current_spend_usd: 400,
  spend_limit_reached: true,
  has_limit: true,
  ceilings: [
    {
      scope: 'principal',
      id: HUMAN.id,
      window: 'monthly',
      limit_usd: 400,
      spend_usd: 400,
      reached: true,
    },
  ],
};

export const BUDGET_IDENTITIES: Record<string, PrincipalIdentity> = {
  [HUMAN.id]: { email: 'dev@kora.example', display_name: 'Dev Operator' },
  usr_9c41d2: { email: 'priya.nair@kora.example', display_name: 'Priya Nair' },
  usr_5e07ab: { email: 'tomas.lindqvist@kora.example', display_name: 'Tomas Lindqvist' },
};

function budgetRow(
  seed: Pick<BudgetPrincipalRow, 'principal_id' | 'principal_type'> & Partial<PrincipalIdentity>,
  fields: Pick<
    BudgetPrincipalRow,
    'categories' | 'effective_limit_usd' | 'effective_spend_usd' | 'current_spend_usd'
  > &
    Partial<
      Pick<BudgetPrincipalRow, 'has_limit' | 'spend_limit_reached' | 'effective_reset_cadence'>
    >,
  lastActiveMinutes: number,
): BudgetPrincipalRow {
  const hasLimit = fields.has_limit ?? true;
  return {
    principal_id: seed.principal_id,
    principal_type: seed.principal_type,
    email: seed.email ?? null,
    display_name: seed.display_name ?? null,
    categories: fields.categories,
    effective_limit_usd: fields.effective_limit_usd,
    effective_spend_usd: fields.effective_spend_usd,
    current_spend_usd: fields.current_spend_usd,
    has_limit: hasLimit,
    spend_limit_reached: fields.spend_limit_reached ?? false,
    ...(fields.effective_reset_cadence
      ? { effective_reset_cadence: fields.effective_reset_cadence }
      : {}),
    last_active_at: ago(lastActiveMinutes * MINUTE),
  };
}

export const BUDGET_PRINCIPAL_ROWS: BudgetPrincipalRow[] = [
  budgetRow(
    { principal_id: AGENT_CODE_REVIEWER.id, principal_type: 'AGENT' },
    {
      categories: [
        {
          source: 'grant',
          id: 'e6b4b08d-e5a1-4c27-9f30-2b7d8a1c5e64',
          limit_usd: 1_500,
          reset_cadence: 'monthly',
          per_run_limit_usd: 12,
          spend_usd: 1_284.4,
        },
      ],
      effective_limit_usd: 1_500,
      effective_spend_usd: 1_284.4,
      current_spend_usd: 1_284.4,
      effective_reset_cadence: 'monthly',
    },
    3,
  ),
  budgetRow(
    { principal_id: AGENT_CODER.id, principal_type: 'AGENT' },
    {
      categories: [
        {
          source: 'grant',
          id: '4a90c1e7-3d58-4f2b-b6a9-7e0d2c8f1b35',
          limit_usd: 900,
          reset_cadence: 'monthly',
          spend_usd: 902.15,
        },
      ],
      effective_limit_usd: 900,
      effective_spend_usd: 902.15,
      current_spend_usd: 902.15,
      spend_limit_reached: true,
      effective_reset_cadence: 'monthly',
    },
    7,
  ),
  budgetRow(
    { principal_id: AGENT_SECURITY.id, principal_type: 'AGENT' },
    {
      categories: [
        {
          source: 'grant',
          id: 'b81d3f09-6c42-4e7a-8d15-0a9e5c2b7f46',
          limit_usd: 800,
          reset_cadence: 'monthly',
          spend_usd: 611.9,
        },
      ],
      effective_limit_usd: 800,
      effective_spend_usd: 611.9,
      current_spend_usd: 611.9,
      effective_reset_cadence: 'monthly',
    },
    22,
  ),
  budgetRow(
    {
      principal_id: HUMAN.id,
      principal_type: 'HUMAN',
      email: 'dev@kora.example',
      display_name: 'Dev Operator',
    },
    {
      categories: [
        {
          source: 'role',
          id: 'engineering',
          limit_usd: 2_500,
          reset_cadence: 'weekly',
          pooled: true,
          spend_usd: 2_140.25,
        },
        {
          source: 'role',
          id: 'operator',
          limit_usd: 400,
          reset_cadence: 'monthly',
          spend_usd: 286.4,
        },
      ],
      effective_limit_usd: 2_500,
      effective_spend_usd: 2_140.25,
      current_spend_usd: 286.4,
      effective_reset_cadence: 'weekly',
    },
    38,
  ),
  budgetRow(
    {
      principal_id: 'usr_9c41d2',
      principal_type: 'HUMAN',
      email: 'priya.nair@kora.example',
      display_name: 'Priya Nair',
    },
    {
      categories: [
        {
          source: 'role',
          id: 'engineering',
          limit_usd: 120,
          reset_cadence: 'monthly',
          spend_usd: 41.85,
        },
      ],
      effective_limit_usd: 120,
      effective_spend_usd: 41.85,
      current_spend_usd: 41.85,
      effective_reset_cadence: 'monthly',
    },
    190,
  ),
  budgetRow(
    {
      principal_id: 'usr_5e07ab',
      principal_type: 'HUMAN',
      email: 'tomas.lindqvist@kora.example',
      display_name: 'Tomas Lindqvist',
    },
    {
      categories: [],
      effective_limit_usd: 0,
      effective_spend_usd: 12.6,
      current_spend_usd: 12.6,
      has_limit: false,
    },
    640,
  ),
];

export const BUDGET_PRINCIPALS: BudgetPrincipalsView = {
  period: new Date().toISOString().slice(0, 7),
  tenant_cap: { limit_usd: 6_000, reset_cadence: 'monthly', spend_usd: 3_287.9, reached: false },
  data: BUDGET_PRINCIPAL_ROWS,
};

export const BUDGET_ROLES: RoleEntry[] = [
  {
    id: 'rol_01',
    tenant_id: TENANT,
    role: 'engineering',
    scopes: ['inference:chat', 'agents:read', 'agents:runs:write', 'mcp:github:read'],
    budget: { limit_usd: 2_500, reset_cadence: 'weekly', pooled: true },
    created_at: ago(120 * DAY),
  },
  {
    id: 'rol_02',
    tenant_id: TENANT,
    role: 'operator',
    scopes: ['inference:chat', 'agents:read', 'agents:write', 'platform:budgets:read'],
    budget: { limit_usd: 400, reset_cadence: 'monthly' },
    created_at: ago(120 * DAY),
  },
  {
    id: 'rol_03',
    tenant_id: TENANT,
    role: 'reviewer',
    scopes: ['inference:chat', 'agents:read'],
    budget: null,
    created_at: ago(64 * DAY),
  },
  {
    id: 'rol_04',
    tenant_id: TENANT,
    role: 'platform-admin',
    scopes: ['platform:budgets:read', 'platform:budgets:write', 'platform:grants:read'],
    budget: null,
    created_at: ago(120 * DAY),
  },
];

export const BUDGET_GRANTS: Grant[] = [
  {
    id: 'e6b4b08d-e5a1-4c27-9f30-2b7d8a1c5e64',
    tenant_id: TENANT,
    principal_type: 'AGENT',
    principal_id: AGENT_CODE_REVIEWER.id,
    scopes: ['inference:chat', 'mcp:github:read', 'mcp:github:write'],
    budget: { limit_usd: 1_500, reset_cadence: 'monthly', per_run_limit_usd: 12 },
    created_at: ago(70 * DAY),
  },
  {
    id: '4a90c1e7-3d58-4f2b-b6a9-7e0d2c8f1b35',
    tenant_id: TENANT,
    principal_type: 'AGENT',
    principal_id: AGENT_CODER.id,
    scopes: ['inference:chat', 'agents:runs:write', 'mcp:github:read'],
    budget: { limit_usd: 900, reset_cadence: 'monthly' },
    created_at: ago(52 * DAY),
  },
  {
    id: 'b81d3f09-6c42-4e7a-8d15-0a9e5c2b7f46',
    tenant_id: TENANT,
    principal_type: 'AGENT',
    principal_id: AGENT_SECURITY.id,
    scopes: ['inference:chat', 'mcp:github:read'],
    budget: { limit_usd: 800, reset_cadence: 'monthly' },
    created_at: ago(33 * DAY),
  },
  {
    id: 'c3f20a58-91d7-4b6e-a804-5d1e7b9c0f23',
    tenant_id: TENANT,
    principal_type: 'HUMAN',
    principal_id: 'usr_9c41d2',
    scopes: ['inference:chat', 'agents:read'],
    budget: null,
    created_at: ago(18 * DAY),
  },
];
