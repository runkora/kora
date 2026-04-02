// ─── Model Gateway Mock Data ──────────────────────────────────────────────────

// ── Model types ───────────────────────────────────────────────────────────────

export type Provider = 'Anthropic' | 'OpenAI' | 'Google' | 'Mistral' | 'Meta';
export type ModelStatus = 'available' | 'unavailable';

export interface ModelRow {
  id: string;
  provider: Provider;
  name: string;
  status: ModelStatus;
  latencyMs: number;
  contextK: number; // context window in thousands of tokens
  promptPer1M: number; // USD per 1M prompt tokens
  outputPer1M: number; // USD per 1M output tokens
  requestCost: number; // USD per request (avg)
  favourite: boolean;
  top?: boolean;
  topLabel?: string;
}

export const MODELS: ModelRow[] = [
  {
    id: 'claude-opus-4-5',
    provider: 'Anthropic',
    name: 'Claude Opus 4.5',
    status: 'available',
    latencyMs: 1420,
    contextK: 200,
    promptPer1M: 15.0,
    outputPer1M: 75.0,
    requestCost: 0.0432,
    favourite: true,
    top: true,
    topLabel: 'Most Capable',
  },
  {
    id: 'claude-sonnet-4-5',
    provider: 'Anthropic',
    name: 'Claude Sonnet 4.5',
    status: 'available',
    latencyMs: 820,
    contextK: 200,
    promptPer1M: 3.0,
    outputPer1M: 15.0,
    requestCost: 0.0089,
    favourite: true,
    top: true,
    topLabel: 'Best Value',
  },
  {
    id: 'claude-haiku-3-5',
    provider: 'Anthropic',
    name: 'Claude Haiku 3.5',
    status: 'available',
    latencyMs: 380,
    contextK: 200,
    promptPer1M: 0.8,
    outputPer1M: 4.0,
    requestCost: 0.0024,
    favourite: false,
  },
  {
    id: 'gpt-4o',
    provider: 'OpenAI',
    name: 'GPT-4o',
    status: 'available',
    latencyMs: 760,
    contextK: 128,
    promptPer1M: 2.5,
    outputPer1M: 10.0,
    requestCost: 0.0071,
    favourite: true,
    top: true,
    topLabel: 'Fastest',
  },
  {
    id: 'gpt-4o-mini',
    provider: 'OpenAI',
    name: 'GPT-4o Mini',
    status: 'available',
    latencyMs: 290,
    contextK: 128,
    promptPer1M: 0.15,
    outputPer1M: 0.6,
    requestCost: 0.0004,
    favourite: false,
  },
  {
    id: 'o3',
    provider: 'OpenAI',
    name: 'o3',
    status: 'available',
    latencyMs: 4200,
    contextK: 200,
    promptPer1M: 10.0,
    outputPer1M: 40.0,
    requestCost: 0.028,
    favourite: false,
  },
  {
    id: 'o4-mini',
    provider: 'OpenAI',
    name: 'o4-mini',
    status: 'available',
    latencyMs: 1100,
    contextK: 200,
    promptPer1M: 1.1,
    outputPer1M: 4.4,
    requestCost: 0.0031,
    favourite: false,
  },
  {
    id: 'gemini-2-5-pro',
    provider: 'Google',
    name: 'Gemini 2.5 Pro',
    status: 'available',
    latencyMs: 1050,
    contextK: 1000,
    promptPer1M: 1.25,
    outputPer1M: 10.0,
    requestCost: 0.0055,
    favourite: false,
  },
  {
    id: 'gemini-2-5-flash',
    provider: 'Google',
    name: 'Gemini 2.5 Flash',
    status: 'available',
    latencyMs: 410,
    contextK: 1000,
    promptPer1M: 0.15,
    outputPer1M: 0.6,
    requestCost: 0.0004,
    favourite: false,
  },
  {
    id: 'mistral-large-2',
    provider: 'Mistral',
    name: 'Mistral Large 2',
    status: 'available',
    latencyMs: 680,
    contextK: 128,
    promptPer1M: 2.0,
    outputPer1M: 6.0,
    requestCost: 0.0048,
    favourite: false,
  },
  {
    id: 'mistral-small-3-1',
    provider: 'Mistral',
    name: 'Mistral Small 3.1',
    status: 'unavailable',
    latencyMs: 0,
    contextK: 128,
    promptPer1M: 0.1,
    outputPer1M: 0.3,
    requestCost: 0.0002,
    favourite: false,
  },
  {
    id: 'llama-3-3-70b',
    provider: 'Meta',
    name: 'Llama 3.3 70B',
    status: 'available',
    latencyMs: 540,
    contextK: 128,
    promptPer1M: 0.23,
    outputPer1M: 0.4,
    requestCost: 0.0008,
    favourite: false,
  },
];

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

// 30 days of mock spend data
export const DAILY_SPEND: DailySpendRow[] = [
  { date: '2025-06-01', totalUsd: 4.21, promptUsd: 2.1, outputUsd: 2.11 },
  { date: '2025-06-02', totalUsd: 5.87, promptUsd: 2.94, outputUsd: 2.93 },
  { date: '2025-06-03', totalUsd: 3.44, promptUsd: 1.72, outputUsd: 1.72 },
  { date: '2025-06-04', totalUsd: 6.19, promptUsd: 3.1, outputUsd: 3.09 },
  { date: '2025-06-05', totalUsd: 7.82, promptUsd: 3.91, outputUsd: 3.91 },
  { date: '2025-06-06', totalUsd: 5.03, promptUsd: 2.52, outputUsd: 2.51 },
  { date: '2025-06-07', totalUsd: 2.11, promptUsd: 1.06, outputUsd: 1.05 },
  { date: '2025-06-08', totalUsd: 8.64, promptUsd: 4.32, outputUsd: 4.32 },
  { date: '2025-06-09', totalUsd: 9.21, promptUsd: 4.61, outputUsd: 4.6 },
  { date: '2025-06-10', totalUsd: 6.77, promptUsd: 3.39, outputUsd: 3.38 },
  { date: '2025-06-11', totalUsd: 5.5, promptUsd: 2.75, outputUsd: 2.75 },
  { date: '2025-06-12', totalUsd: 4.98, promptUsd: 2.49, outputUsd: 2.49 },
  { date: '2025-06-13', totalUsd: 7.34, promptUsd: 3.67, outputUsd: 3.67 },
  { date: '2025-06-14', totalUsd: 3.12, promptUsd: 1.56, outputUsd: 1.56 },
  { date: '2025-06-15', totalUsd: 6.45, promptUsd: 3.23, outputUsd: 3.22 },
  { date: '2025-06-16', totalUsd: 8.9, promptUsd: 4.45, outputUsd: 4.45 },
  { date: '2025-06-17', totalUsd: 11.23, promptUsd: 5.62, outputUsd: 5.61 },
  { date: '2025-06-18', totalUsd: 9.44, promptUsd: 4.72, outputUsd: 4.72 },
  { date: '2025-06-19', totalUsd: 7.01, promptUsd: 3.51, outputUsd: 3.5 },
  { date: '2025-06-20', totalUsd: 5.88, promptUsd: 2.94, outputUsd: 2.94 },
  { date: '2025-06-21', totalUsd: 4.33, promptUsd: 2.17, outputUsd: 2.16 },
  { date: '2025-06-22', totalUsd: 6.77, promptUsd: 3.39, outputUsd: 3.38 },
  { date: '2025-06-23', totalUsd: 8.12, promptUsd: 4.06, outputUsd: 4.06 },
  { date: '2025-06-24', totalUsd: 10.54, promptUsd: 5.27, outputUsd: 5.27 },
  { date: '2025-06-25', totalUsd: 9.08, promptUsd: 4.54, outputUsd: 4.54 },
  { date: '2025-06-26', totalUsd: 7.44, promptUsd: 3.72, outputUsd: 3.72 },
  { date: '2025-06-27', totalUsd: 6.21, promptUsd: 3.11, outputUsd: 3.1 },
  { date: '2025-06-28', totalUsd: 5.09, promptUsd: 2.55, outputUsd: 2.54 },
  { date: '2025-06-29', totalUsd: 8.77, promptUsd: 4.39, outputUsd: 4.38 },
  { date: '2025-06-30', totalUsd: 12.41, promptUsd: 6.21, outputUsd: 6.2 },
];

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
