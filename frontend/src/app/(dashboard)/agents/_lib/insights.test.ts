import { describe, expect, it } from 'vitest';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import { toBudgetRows, toPeriodData, toTenantBudget, trendLabel } from './insights';

const RESPONSE: AgentInsightsResponse = {
  period: '7d',
  start: '2026-09-27T14:00:00Z',
  end: '2026-10-04T14:00:00Z',
  bucket_seconds: 86400,
  usage: [
    {
      agent_slug: 'coder',
      prompt_tokens: 10,
      completion_tokens: 5,
      cache_read_tokens: 1,
      cache_creation_tokens: 2,
      total_tokens: 18,
      estimated_cost_usd: 1.5,
    },
  ],
  total_cost_usd: 1.5,
  previous_total_usd: 1,
  models: [{ model_id: 'claude-sonnet', cost_usd: 1.5 }],
  trend: [
    { start: '2026-09-27T14:00:00Z', cost_usd: 0.5 },
    { start: '2026-09-28T14:00:00Z', cost_usd: 1 },
  ],
  all_delegated_usage: false,
  outcomes: { completed: 4, failed: 1 },
};

describe('trendLabel', () => {
  it('reads each label in local time, as a clock time, a weekday or a date', () => {
    const start = new Date('2026-10-03T12:00:00Z');
    const hh = String(start.getHours()).padStart(2, '0');
    const mm = String(start.getMinutes()).padStart(2, '0');
    expect(trendLabel('24h', start.toISOString())).toBe(`${hh}:${mm}`);
    expect(trendLabel('7d', start.toISOString())).toBe(
      start.toLocaleDateString('en-US', { weekday: 'short' }),
    );
    expect(trendLabel('90d', start.toISOString())).toBe(
      start.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }),
    );
  });
});

describe('toPeriodData', () => {
  it('keys usage by agent slug and carries every part across', () => {
    const data = toPeriodData('7d', RESPONSE);
    expect(Object.keys(data.usage)).toEqual(['coder']);
    expect(data.total).toBe(1.5);
    expect(data.outcomes).toEqual({ completed: 4, failed: 1 });
    expect(data.models).toEqual([{ name: 'claude-sonnet', cost: 1.5 }]);
    expect(data.trend?.values).toEqual([0.5, 1]);
    expect(data.previousTotal).toBe(1);
  });

  it('marks delegated usage as the caller own unless the caller sees every row', () => {
    expect(toPeriodData('7d', RESPONSE).delegatedOwnOnly).toBe(true);
    expect(toPeriodData('7d', { ...RESPONSE, all_delegated_usage: true }).delegatedOwnOnly).toBe(
      false,
    );
  });

  it('leaves outcomes out when the BFF could not count runs', () => {
    const withoutOutcomes: AgentInsightsResponse = { ...RESPONSE };
    delete withoutOutcomes.outcomes;
    expect('outcomes' in toPeriodData('7d', withoutOutcomes)).toBe(false);
  });
});

describe('toBudgetRows', () => {
  const agents = [
    { slug: 'coder', name: 'Coder', identityMode: 'autonomous' },
    { slug: 'triage', name: 'Triage Bot', identityMode: 'autonomous' },
    { slug: 'notes', name: 'Notes', identityMode: 'delegated' },
  ];
  const win = (cadence: 'weekly' | 'monthly', limit_usd: number, spend_usd: number) => ({
    cadence,
    limit_usd,
    spend_usd,
    resets_at: '2026-10-05T00:00:00Z',
  });

  it('keeps every window, the closest to a limit first, and drops delegated agents', () => {
    const rows = toBudgetRows(
      [
        { agent_slug: 'coder', per_run_limit_usd: 5, windows: [win('monthly', 100, 40)] },
        { agent_slug: 'triage', windows: [win('weekly', 20, 3), win('monthly', 80, 70)] },
        { agent_slug: 'notes', windows: [win('monthly', 10, 9)] },
        { agent_slug: 'gone', windows: [win('monthly', 10, 1)] },
      ],
      agents,
    );
    expect(rows.map((r) => r.name)).toEqual(['Triage Bot', 'Coder', 'gone']);
    expect(rows[0]?.windows.map((w) => [w.cadence, w.limit, w.spent])).toEqual([
      ['weekly', 20, 3],
      ['monthly', 80, 70],
    ]);
    expect(rows[1]?.perRunLimit).toBe(5);
    expect(rows[0] && 'perRunLimit' in rows[0]).toBe(false);
  });
});

describe('toTenantBudget', () => {
  it('counts the days left in a monthly window', () => {
    const cap = { limit_usd: 1000, reset_cadence: 'monthly', spend_usd: 400 };
    expect(toTenantBudget(cap, new Date('2026-10-03T14:00:00Z'))).toEqual({
      limit: 1000,
      spent: 400,
      daysLeft: 29,
    });
  });

  it('counts the days to the next Monday for a weekly cap', () => {
    const cap = { limit_usd: 100, reset_cadence: 'weekly', spend_usd: 10 };
    expect(toTenantBudget(cap, new Date('2026-10-03T14:00:00Z'))?.daysLeft).toBe(2);
    expect(toTenantBudget(cap, new Date('2026-09-28T09:00:00Z'))?.daysLeft).toBe(7);
    expect(toTenantBudget(cap, new Date('2026-10-03T14:00:00Z'))?.cadence).toBe('weekly');
  });

  it('is absent without a cap or with a zero limit', () => {
    expect(toTenantBudget(undefined, new Date())).toBeUndefined();
    expect(
      toTenantBudget({ limit_usd: 0, reset_cadence: 'monthly', spend_usd: 5 }, new Date()),
    ).toBeUndefined();
  });
});
