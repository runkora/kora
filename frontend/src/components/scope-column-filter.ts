/**
 * @tier 2 — Orbit-domain-aware: scope strings.
 *
 * Builds the `DataTable` column filter used on every scope column: a
 * searchable, multi-select list of the distinct scopes present in the data.
 * Selecting several scopes keeps any row that carries at least one of them
 * (union).
 */
import type { DataTableClientFilter } from '@orbit/hades';

export function scopeColumnFilter<TData>(
  rows: readonly TData[],
  scopesOf: (row: TData) => readonly string[],
): DataTableClientFilter<TData> {
  const distinct = new Set<string>();
  for (const row of rows) for (const scope of scopesOf(row)) distinct.add(scope);

  return {
    mode: 'client',
    searchable: true,
    mono: true,
    options: [...distinct].sort().map((scope) => ({ value: scope, label: scope })),
    filterFn: (row, selected) => scopesOf(row).some((scope) => selected.has(scope)),
  };
}
