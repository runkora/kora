import { describe, expect, it } from 'vitest';
import type { AgentBudgetsResponse, AgentInsightsResponse } from '@orbit/shared/api';
import { toBudgetRows, toPeriodData, toTenantBudget, trendLabel } from './insights';

const RESPONSE: AgentInsightsResponse = {
  period: '7d',
  start: '2026-09-27T00:00:00Z',
  end: '2026-10-04T00:00:00Z',
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
    { start: '2026-09-27T00:00:00Z', cost_usd: 0.5 },
    { start: '2026-09-28T00:00:00Z', cost_usd: 1 },
  ],
  outcomes: { completed: 4, failed: 1 },
};

describe('trendLabel', () => {
  it('names a daily bucket by its UTC weekday', () => {
    expect(trendLabel('7d', '2026-09-28T00:00:00Z')).toBe('Mon');
  });

  it('names a longer bucket by its UTC date', () => {
    expect(trendLabel('30d', '2026-09-04T00:00:00Z')).toBe('Sep 4');
    expect(trendLabel('90d', '2026-07-06T00:00:00Z')).toBe('Jul 6');
  });

  it('names a 3-hour bucket by its local clock time', () => {
    const start = new Date('2026-10-03T12:00:00Z');
    const hh = String(start.getHours()).padStart(2, '0');
    const mm = String(start.getMinutes()).padStart(2, '0');
    expect(trendLabel('24h', start.toISOString())).toBe(`${hh}:${mm}`);
  });
});

describe('toPeriodData', () => {
  it('keys usage by agent slug and carries every part across', () => {
    const data = toPeriodData('7d', RESPONSE);
    expect(Object.keys(data.usage)).toEqual(['coder']);
    expect(data.outcomes).toEqual({ completed: 4, failed: 1 });
    expect(data.models).toEqual([{ name: 'claude-sonnet', cost: 1.5 }]);
    expect(data.trend).toEqual({ labels: ['Sun', 'Mon'], values: [0.5, 1] });
    expect(data.previousTotal).toBe(1);
  });

  it('leaves outcomes out when the BFF could not count runs', () => {
    const withoutOutcomes: AgentInsightsResponse = { ...RESPONSE };
    delete withoutOutcomes.outcomes;
    expect('outcomes' in toPeriodData('7d', withoutOutcomes)).toBe(false);
  });
});

describe('toBudgetRows', () => {
  const agents = [
    { slug: 'coder', name: 'Coder' },
    { slug: 'triage', name: 'Triage Bot' },
  ];
  const resp: AgentBudgetsResponse = {
    agents: [
      {
        agent_slug: 'triage',
        week_to_date_usd: 3,
        month_to_date_usd: 9,
        limit_usd: 20,
        reset_cadence: 'weekly',
      },
      {
        agent_slug: 'coder',
        week_to_date_usd: 5,
        month_to_date_usd: 40,
        limit_usd: 100,
        reset_cadence: 'monthly',
      },
      { agent_slug: 'gone', week_to_date_usd: 1, month_to_date_usd: 2 },
    ],
  };

  it('reports the spend in each agent’s own budget window, largest first', () => {
    expect(toBudgetRows(resp, agents)).toEqual([
      { name: 'Coder', spent: 40, limit: 100 },
      { name: 'Triage Bot', spent: 3, limit: 20, cadence: 'weekly' },
      { name: 'gone', spent: 2 },
    ]);
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
    // Saturday: Sunday and Monday morning are 2 days off.
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
