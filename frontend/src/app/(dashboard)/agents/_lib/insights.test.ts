import { describe, expect, it } from 'vitest';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import { toBudgetRows, toPeriodData, toTenantBudget } from './insights';

const win = (cadence: 'weekly' | 'monthly', limit_usd: number, spend_usd: number) => ({
  cadence,
  limit_usd,
  spend_usd,
  resets_at: '2026-10-05T00:00:00Z',
});

describe('toBudgetRows', () => {
  const agents = [
    { slug: 'coder', name: 'Coder', identityMode: 'autonomous' },
    { slug: 'triage', name: 'Triage Bot', identityMode: 'autonomous' },
    { slug: 'notes', name: 'Notes', identityMode: 'delegated' },
  ];

  it('keeps every window, the closest to a limit first, and drops delegated agents', () => {
    const rows = toBudgetRows(
      [
        { agent_slug: 'coder', per_run_limit_usd: 5, windows: [win('monthly', 100, 40)] },
        { agent_slug: 'triage', windows: [win('weekly', 20, 3), win('monthly', 80, 70)] },
        { agent_slug: 'notes', windows: [win('monthly', 10, 9)] },
      ],
      agents,
    );
    expect(rows.map((r) => r.name)).toEqual(['Triage Bot', 'Coder']);
    expect(rows[0]?.windows.map((w) => [w.cadence, w.limit, w.spent])).toEqual([
      ['weekly', 20, 3],
      ['monthly', 80, 70],
    ]);
    expect(rows[1]?.perRunLimit).toBe(5);
  });
});

describe('toPeriodData', () => {
  it('reads delegated usage as the caller own unless the caller sees every row', () => {
    const response = { all_delegated_usage: false, usage: [], models: [], trend: [] };
    expect(toPeriodData('7d', response as unknown as AgentInsightsResponse).delegatedOwnOnly).toBe(
      true,
    );
    expect(
      toPeriodData('7d', {
        ...response,
        all_delegated_usage: true,
      } as unknown as AgentInsightsResponse).delegatedOwnOnly,
    ).toBe(false);
  });
});

describe('toTenantBudget', () => {
  it('counts the days to the reset, and is absent for a zero limit', () => {
    const cap = {
      limit_usd: 1000,
      reset_cadence: 'monthly',
      spend_usd: 400,
      resets_at: '2026-11-01T00:00:00Z',
    };
    expect(toTenantBudget(cap, new Date('2026-10-03T14:00:00Z'))?.daysLeft).toBe(29);
    expect(toTenantBudget({ ...cap, limit_usd: 0 }, new Date())).toBeUndefined();
  });
});
