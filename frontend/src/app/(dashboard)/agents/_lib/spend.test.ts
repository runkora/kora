import { describe, expect, it } from 'vitest';
import { closestToLimit, groupTail } from './spend';

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

  it('leaves a short list alone', () => {
    expect(groupTail(rows.slice(0, 6))).toEqual(rows.slice(0, 6));
    expect(groupTail([])).toEqual([]);
  });
});

describe('closestToLimit', () => {
  it('orders by share of limit, then puts unbudgeted agents last by spend', () => {
    const ordered = closestToLimit([
      { name: 'free-big', spent: 90 },
      { name: 'low', spent: 10, limit: 100 },
      { name: 'free-small', spent: 1 },
      { name: 'over', spent: 120, limit: 100 },
      { name: 'high', spent: 80, limit: 100 },
    ]);
    expect(ordered.map((r) => r.name)).toEqual(['over', 'high', 'low', 'free-big', 'free-small']);
  });
});
