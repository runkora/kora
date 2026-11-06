/**
 * @tier 3 — triggers-scoped. Pure helpers for editing a trigger in place.
 *
 * The modal edits a stored trigger rather than a draft, so it needs the
 * inverse of the wizard's builders: stored filters back into editable rows, stored manual inputs into a template catalog, and a cron
 * expression checked before it is sent. Kept free of React so each rule is
 * unit-testable on its own.
 */

import type { InputField } from '@runkora/shared/api';
import { inputsToCatalog } from './trigger-wizard/field-catalog';
import type { FieldCatalogEntry } from './trigger-wizard/types';
import type { FilterRow } from './filter-rows-editor';
import { matchConditionFrom, matchContainsFrom } from './trigger-wizard/wizard-state';

/**
 * Turns a stored `match_condition` and `match_contains` back into editable rows.
 *
 * Order is the objects' own, `is` rows first; there is no meaningful sort for
 * clauses that all have to match anyway.
 */
export function filtersFromMatchCondition(
  condition: Record<string, string[]> | undefined,
  contains?: Record<string, string[]>,
): FilterRow[] {
  const rows = (entries: Record<string, string[]> | undefined, operator: FilterRow['operator']) =>
    Object.entries(entries ?? {}).map(([field, values]) => ({
      field,
      operator,
      values: values.join(' | '),
    }));
  return [...rows(condition, 'is'), ...rows(contains, 'contains')];
}

/**
 * The template fields a manual trigger's prompt can reference.
 *
 * The wizard derives these from its own draft fields; a stored trigger already
 * has the resolved keys, so this reads them straight off the declaration.
 */
export function manualInputsCatalog(inputs: InputField[] | undefined): FieldCatalogEntry[] {
  return inputsToCatalog(
    (inputs ?? []).map((input) => ({
      name: input.name,
      ...(input.label !== undefined && { label: input.label }),
      type: input.type,
      ...(input.choices !== undefined && { choices: input.choices }),
      ...(input.default !== undefined && { default: input.default }),
    })),
  );
}

/**
 * A saved filter draft as the platform stores it: rows for one field folded into one, repeated
 * values dropped, blank rows gone. The modal shows this once a save lands.
 */
export function normalizeFilters<T extends { filters: FilterRow[] }>(value: T): T {
  return {
    ...value,
    filters: filtersFromMatchCondition(
      matchConditionFrom(value.filters),
      matchContainsFrom(value.filters),
    ),
  };
}

/** Whether two filter row lists carry the same content. */
export function sameFilters(a: FilterRow[], b: FilterRow[]): boolean {
  if (a.length !== b.length) return false;
  return a.every(
    (row, i) =>
      row.field === b[i]?.field &&
      (row.operator ?? 'is') === (b[i]?.operator ?? 'is') &&
      row.values === b[i]?.values,
  );
}
