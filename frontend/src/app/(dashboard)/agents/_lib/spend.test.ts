import { describe, expect, it } from 'vitest';
import { closestToLimit, groupTail, isPeriod } from './spend';
import type { BudgetRow } from './spend';

const row = (name: string, ...windows: [number, number][]): BudgetRow => ({
  name,
  windows: windows.map(([limit, spent]) => ({
    cadence: 'monthly' as const,
    limit,
    spent,
    resetsAt: '2026-11-01T00:00:00Z',
  })),
});

describe('groupTail', () => {
  it('folds everything past the legend size into one Other row and keeps the total', () => {
    const rows = Array.from({ length: 150 }, (_, i) => ({ name: `a${i}`, cost: 150 - i }));
    const grouped = groupTail(rows);
    expect(grouped.map((r) => r.name)).toEqual(['a0', 'a1', 'a2', 'a3', 'a4', 'Other (145)']);
    const sum = (rs: { cost: number }[]) => rs.reduce((n, r) => n + r.cost, 0);
    expect(sum(grouped)).toBe(sum(rows));
  });
});

describe('closestToLimit', () => {
  it('orders by the tightest window of each agent, a zero limit counting as fully spent', () => {
    const ordered = closestToLimit([
      row('low', [100, 10]),
      row('two windows', [100, 10], [50, 45]),
      row('over', [100, 120]),
      row('zero', [0, 0]),
    ]);
    expect(ordered.map((r) => r.name)).toEqual(['zero', 'over', 'two windows', 'low']);
  });
});

describe('isPeriod', () => {
  it('accepts the four windows and nothing else, including inherited object keys', () => {
    expect(['24h', '7d', '30d', '90d'].every(isPeriod)).toBe(true);
    expect(isPeriod('1y')).toBe(false);
    expect(isPeriod('constructor')).toBe(false);
  });
});
