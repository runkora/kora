import { describe, expect, it } from 'vitest';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import { toBudgetRows, toPeriodData, toTenantBudget } from './insights';

const RESPONSE: AgentInsightsResponse = {
  period: '7d',
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

describe('toPeriodData', () => {
  it('marks delegated usage as the caller own unless the caller sees every row', () => {
    expect(toPeriodData('7d', RESPONSE).delegatedOwnOnly).toBe(true);
    expect(toPeriodData('7d', { ...RESPONSE, all_delegated_usage: true }).delegatedOwnOnly).toBe(
      false,
    );
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
  it('counts the days to the cap reset, and is absent without a cap or with a zero limit', () => {
    const cap = {
      limit_usd: 1000,
      reset_cadence: 'monthly',
      spend_usd: 400,
      resets_at: '2026-11-01T00:00:00Z',
    };
    expect(toTenantBudget(cap, new Date('2026-10-03T14:00:00Z'))).toEqual({
      limit: 1000,
      spent: 400,
      daysLeft: 29,
    });
    expect(toTenantBudget(undefined, new Date())).toBeUndefined();
    expect(toTenantBudget({ ...cap, limit_usd: 0 }, new Date())).toBeUndefined();
  });
});
