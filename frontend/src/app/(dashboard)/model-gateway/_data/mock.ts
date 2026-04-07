// ── Usage / time-series data ───────────────────────────────────────────────────

export interface DailySpendRow {
  date: string;
  totalUsd: number;
  promptUsd: number;
  outputUsd: number;
}

export interface DailyTokenRow {
  date: string;
  promptTokens: number;
  outputTokens: number;
  totalTokens: number;
}

export interface DailyRequestRow {
  date: string;
  total: number;
  successful: number;
  errored: number;
}

// ── Date helpers ──────────────────────────────────────────────────────────────

function daysAgo(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return d.toISOString().slice(0, 10);
}

// ── 30 days of mock spend data (relative to today) ────────────────────────────

const SPEND_VALUES = [
  { totalUsd: 4.21, promptUsd: 2.1, outputUsd: 2.11 },
  { totalUsd: 5.87, promptUsd: 2.94, outputUsd: 2.93 },
  { totalUsd: 3.44, promptUsd: 1.72, outputUsd: 1.72 },
  { totalUsd: 6.19, promptUsd: 3.1, outputUsd: 3.09 },
  { totalUsd: 7.82, promptUsd: 3.91, outputUsd: 3.91 },
  { totalUsd: 5.03, promptUsd: 2.52, outputUsd: 2.51 },
  { totalUsd: 2.11, promptUsd: 1.06, outputUsd: 1.05 },
  { totalUsd: 8.64, promptUsd: 4.32, outputUsd: 4.32 },
  { totalUsd: 9.21, promptUsd: 4.61, outputUsd: 4.6 },
  { totalUsd: 6.77, promptUsd: 3.39, outputUsd: 3.38 },
  { totalUsd: 5.5, promptUsd: 2.75, outputUsd: 2.75 },
  { totalUsd: 4.98, promptUsd: 2.49, outputUsd: 2.49 },
  { totalUsd: 7.34, promptUsd: 3.67, outputUsd: 3.67 },
  { totalUsd: 3.12, promptUsd: 1.56, outputUsd: 1.56 },
  { totalUsd: 6.45, promptUsd: 3.23, outputUsd: 3.22 },
  { totalUsd: 8.9, promptUsd: 4.45, outputUsd: 4.45 },
  { totalUsd: 11.23, promptUsd: 5.62, outputUsd: 5.61 },
  { totalUsd: 9.44, promptUsd: 4.72, outputUsd: 4.72 },
  { totalUsd: 7.01, promptUsd: 3.51, outputUsd: 3.5 },
  { totalUsd: 5.88, promptUsd: 2.94, outputUsd: 2.94 },
  { totalUsd: 4.33, promptUsd: 2.17, outputUsd: 2.16 },
  { totalUsd: 6.77, promptUsd: 3.39, outputUsd: 3.38 },
  { totalUsd: 8.12, promptUsd: 4.06, outputUsd: 4.06 },
  { totalUsd: 10.54, promptUsd: 5.27, outputUsd: 5.27 },
  { totalUsd: 9.08, promptUsd: 4.54, outputUsd: 4.54 },
  { totalUsd: 7.44, promptUsd: 3.72, outputUsd: 3.72 },
  { totalUsd: 6.21, promptUsd: 3.11, outputUsd: 3.1 },
  { totalUsd: 5.09, promptUsd: 2.55, outputUsd: 2.54 },
  { totalUsd: 8.77, promptUsd: 4.39, outputUsd: 4.38 },
  { totalUsd: 12.41, promptUsd: 6.21, outputUsd: 6.2 },
];

export const DAILY_SPEND: DailySpendRow[] = SPEND_VALUES.map((v, i) => ({
  date: daysAgo(SPEND_VALUES.length - 1 - i),
  ...v,
}));

// 30 days of mock token data
export const DAILY_TOKENS: DailyTokenRow[] = DAILY_SPEND.map((d) => ({
  date: d.date,
  promptTokens: Math.round(d.promptUsd * 140_000),
  outputTokens: Math.round(d.outputUsd * 14_000),
  totalTokens: Math.round(d.promptUsd * 140_000) + Math.round(d.outputUsd * 14_000),
}));

// 30 days of mock request data
export const DAILY_REQUESTS: DailyRequestRow[] = DAILY_SPEND.map((d) => {
  const total = Math.round((d.totalUsd / 0.0089) * 1.4);
  const errored = Math.round(total * 0.02);
  return { date: d.date, total, successful: total - errored, errored };
});

// ── Quick-stat aggregates ─────────────────────────────────────────────────────

const sumSpend = (rows: DailySpendRow[]) => rows.reduce((s, r) => s + r.totalUsd, 0);

// Current billing period = current month (June 2025)
export const STATS = {
  spend: {
    period: sumSpend(DAILY_SPEND), // current billing period
    month: sumSpend(DAILY_SPEND), // same as period for June
    quarter: sumSpend(DAILY_SPEND) * 2.8, // Q2 approximation
    year: sumSpend(DAILY_SPEND) * 8.1, // YTD approximation
  },
  requests: {
    total: DAILY_REQUESTS.reduce((s, r) => s + r.total, 0),
    successful: DAILY_REQUESTS.reduce((s, r) => s + r.successful, 0),
    errored: DAILY_REQUESTS.reduce((s, r) => s + r.errored, 0),
  },
  tokens: {
    period: DAILY_TOKENS.reduce((s, r) => s + r.totalTokens, 0),
    month: DAILY_TOKENS.reduce((s, r) => s + r.totalTokens, 0),
    quarter: DAILY_TOKENS.reduce((s, r) => s + r.totalTokens, 0) * 2.8,
    year: DAILY_TOKENS.reduce((s, r) => s + r.totalTokens, 0) * 8.1,
  },
};

// ── Latency data ──────────────────────────────────────────────────────────────

// 14 days of mock latency data (deterministic — no Math.random())
export interface DailyLatencyRow {
  date: string;
  p50: number;
  p95: number;
  p99: number;
}

const LATENCY_VALUES = [
  { p50: 118, p95: 198, p99: 312 },
  { p50: 124, p95: 204, p99: 298 },
  { p50: 131, p95: 211, p99: 334 },
  { p50: 115, p95: 195, p99: 287 },
  { p50: 108, p95: 188, p99: 271 },
  { p50: 122, p95: 202, p99: 318 },
  { p50: 139, p95: 219, p99: 351 },
  { p50: 147, p95: 227, p99: 368 },
  { p50: 133, p95: 213, p99: 342 },
  { p50: 121, p95: 201, p99: 309 },
  { p50: 116, p95: 196, p99: 294 },
  { p50: 128, p95: 208, p99: 327 },
  { p50: 142, p95: 222, p99: 356 },
  { p50: 136, p95: 216, p99: 344 },
];

export const DAILY_LATENCY: DailyLatencyRow[] = LATENCY_VALUES.map((v, i) => ({
  date: daysAgo(LATENCY_VALUES.length - 1 - i),
  ...v,
}));

// ── Cost breakdown by model ───────────────────────────────────────────────────

// Cost breakdown by model for the current billing period
export interface ModelCostRow {
  model: string;
  cost: number;
}

export const MODEL_COSTS: ModelCostRow[] = [
  { model: 'claude-3-5-sonnet', cost: 72.14 },
  { model: 'claude-3-5-haiku', cost: 53.88 },
  { model: 'claude-opus-4', cost: 41.22 },
  { model: 'gpt-4o', cost: 21.07 },
  { model: 'llama-3-70b', cost: 9.43 },
];

// ── Top models by usage ───────────────────────────────────────────────────────

export interface ModelUsageRow {
  model: string;
  requests: number;
  cost: number;
}

export const TOP_MODELS_BY_USAGE: ModelUsageRow[] = [
  { model: 'claude-3-5-sonnet', requests: 45230, cost: 72.14 },
  { model: 'claude-3-5-haiku', requests: 38410, cost: 53.88 },
  { model: 'claude-opus-4', requests: 12840, cost: 41.22 },
  { model: 'gpt-4o', requests: 8920, cost: 21.07 },
  { model: 'llama-3-70b', requests: 6110, cost: 9.43 },
];

// ── Recent log entries ────────────────────────────────────────────────────────

export type LogStatus = 'success' | 'error';

export interface RecentLogEntry {
  id: string;
  model: string;
  status: LogStatus;
  latencyMs: number;
  keyName: string;
  minsAgo: number;
}

export const RECENT_LOGS: RecentLogEntry[] = [
  { id: 'req_01', model: 'claude-3-5-sonnet', status: 'success', latencyMs: 312, keyName: 'prod-api-key', minsAgo: 0 },
  { id: 'req_02', model: 'claude-3-5-haiku', status: 'success', latencyMs: 148, keyName: 'staging-key', minsAgo: 1 },
  { id: 'req_03', model: 'gpt-4o', status: 'error', latencyMs: 5021, keyName: 'prod-api-key', minsAgo: 2 },
  { id: 'req_04', model: 'claude-opus-4', status: 'success', latencyMs: 891, keyName: 'dev-key', minsAgo: 3 },
  { id: 'req_05', model: 'claude-3-5-sonnet', status: 'success', latencyMs: 278, keyName: 'prod-api-key', minsAgo: 4 },
  { id: 'req_06', model: 'llama-3-70b', status: 'success', latencyMs: 203, keyName: 'internal-key', minsAgo: 5 },
  { id: 'req_07', model: 'claude-3-5-haiku', status: 'error', latencyMs: 3100, keyName: 'staging-key', minsAgo: 7 },
  { id: 'req_08', model: 'claude-3-5-sonnet', status: 'success', latencyMs: 334, keyName: 'prod-api-key', minsAgo: 9 },
];
