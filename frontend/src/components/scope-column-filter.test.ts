import { describe, it, expect } from 'vitest';
import { scopeColumnFilter } from './scope-column-filter';

interface Row {
  scopes: string[];
}

const rows: Row[] = [
  { scopes: ['b:read', 'a:read'] },
  { scopes: ['a:read', 'c:write'] },
  { scopes: [] },
];

describe('scopeColumnFilter', () => {
  const filter = scopeColumnFilter(rows, (r) => r.scopes);

  it('lists each distinct scope once, sorted', () => {
    expect(filter.options.map((o) => o.value)).toEqual(['a:read', 'b:read', 'c:write']);
  });

  it('is searchable and monospaced', () => {
    expect(filter.searchable).toBe(true);
    expect(filter.mono).toBe(true);
  });

  it('matches a row that has any selected scope', () => {
    const selected = new Set(['b:read', 'c:write']);
    expect(rows.map((r) => filter.filterFn(r, selected))).toEqual([true, true, false]);
  });
});
