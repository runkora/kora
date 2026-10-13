import { describe, expect, it } from 'vitest';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import { toBudgetRows, toPeriodData, trendLabel } from './insights';

const win = (cadence: 'weekly' | 'monthly', limit_usd: number, spend_usd: number) => ({
  cadence,
  limit_usd,
  spend_usd,
  resets_at: '2026-10-05T00:00:00Z',
});

describe('toBudgetRows', () => {
  it('keeps every window and every agent, the closest to a limit first', () => {
    const rows = toBudgetRows(
      [
        { agent_slug: 'coder', per_run_limit_usd: 5, windows: [win('monthly', 100, 40)] },
        { agent_slug: 'triage', windows: [win('weekly', 20, 3), win('monthly', 80, 70)] },
        { agent_slug: 'notes', windows: [win('monthly', 10, 9)] },
      ],
      [
        { slug: 'coder', name: 'Coder' },
        { slug: 'triage', name: 'Triage Bot' },
        { slug: 'notes', name: 'Notes' },
      ],
    );
    expect(rows.map((r) => r.name)).toEqual(['Notes', 'Triage Bot', 'Coder']);
    expect(rows[1]?.windows.map((w) => [w.cadence, w.limit, w.spent])).toEqual([
      ['weekly', 20, 3],
      ['monthly', 80, 70],
    ]);
    expect(rows[2]?.perRunLimit).toBe(5);
  });
});

describe('toPeriodData', () => {
  it('keys usage by agent and reads the model split and the trend', () => {
    const response: AgentInsightsResponse = {
      period: '30d',
      usage: [
        {
          agent_slug: 'coder',
          prompt_tokens: 1,
          completion_tokens: 2,
          cache_read_tokens: 0,
          cache_creation_tokens: 0,
          total_tokens: 3,
          estimated_cost_usd: 1.5,
        },
      ],
      total_cost_usd: 1.5,
      previous_total_usd: 1,
      models: [{ model_id: 'm1', estimated_cost_usd: 1.5 }],
      trend: [{ start: '2026-09-04T09:30:00Z', cost_usd: 1.5 }],
    };
    const data = toPeriodData('30d', response);
    expect(data.usage['coder']?.estimated_cost_usd).toBe(1.5);
    expect(data.models).toEqual([{ name: 'm1', cost: 1.5 }]);
    expect(data.trend).toEqual({ labels: ['Sep 4'], values: [1.5] });
    expect(data.previousTotal).toBe(1);
  });
});

describe('trendLabel', () => {
  it('labels the longer windows by date', () => {
    expect(trendLabel('90d', '2026-07-05T23:59:00Z')).toBe('Jul 5');
  });
});
