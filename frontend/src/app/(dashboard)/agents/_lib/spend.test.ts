import { describe, expect, it } from 'vitest';
import { closestToLimit, groupTail } from './spend';
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
  const rows = Array.from({ length: 150 }, (_, i) => ({ name: `a${i}`, cost: 150 - i }));

  it('folds everything past the legend size into one Other row and keeps the total', () => {
    const grouped = groupTail(rows);
    expect(grouped).toHaveLength(6);
    expect(grouped.slice(0, 5).map((r) => r.name)).toEqual(['a0', 'a1', 'a2', 'a3', 'a4']);
    expect(grouped[5]?.name).toBe('Other (145)');
    const sum = (rs: { cost: number }[]) => rs.reduce((n, r) => n + r.cost, 0);
    expect(sum(grouped)).toBe(sum(rows));
  });
});

describe('closestToLimit', () => {
  it('orders by the tightest window of each agent, then by name', () => {
    const ordered = closestToLimit([
      row('low', [100, 10]),
      row('two windows', [100, 10], [50, 45]),
      row('over', [100, 120]),
      row('b', [100, 50]),
      row('a', [100, 50]),
    ]);
    expect(ordered.map((r) => r.name)).toEqual(['over', 'two windows', 'a', 'b', 'low']);
  });

  it('puts a zero limit first, as fully spent', () => {
    const ordered = closestToLimit([row('over', [100, 120]), row('zero', [0, 0])]);
    expect(ordered.map((r) => r.name)).toEqual(['zero', 'over']);
  });
});
