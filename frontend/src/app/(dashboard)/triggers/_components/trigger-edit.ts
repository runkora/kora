/**
 * @tier 3 — triggers-scoped. Pure helpers for editing a trigger in place.
 *
 * The drawer edits a stored trigger rather than a draft, so it needs the
 * inverse of the wizard's builders: a stored `match_condition` back into
 * editable rows, stored manual inputs into a template catalog, and a cron
 * expression checked before it is sent. Kept free of React so each rule is
 * unit-testable on its own.
 */

import type { InputField } from '@orbit/shared/api';
import { inputsToCatalog } from './trigger-wizard/field-catalog';
import type { FieldCatalogEntry } from './trigger-wizard/types';

/**
 * One editable filter row. Values stay a raw string while being typed.
 *
 * Structurally the wizard's `WizardFilter`: both surfaces edit the same rows
 * through the shared `FilterRowsEditor`.
 */
export interface EditFilterRow {
  field: string;
  values: string;
}

/**
 * Turns a stored `match_condition` back into editable rows.
 *
 * Order is the object's own, which is the order the backend returned; there is
 * no meaningful sort for clauses that all have to match anyway.
 */
export function filtersFromMatchCondition(
  condition: Record<string, string[]> | undefined,
): EditFilterRow[] {
  if (!condition) return [];
  return Object.entries(condition).map(([field, values]) => ({
    field,
    values: values.join(' | '),
  }));
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
 * Whether an expression is a 5-field cron the scheduler will accept.
 *
 * Deliberately shallow: it checks the shape, not the semantics of each field,
 * because the scheduler is the authority on what a field may contain and a
 * stricter guess here would reject expressions that actually work.
 */
export function isValidCron(expr: string): boolean {
  const parts = expr.trim().split(/\s+/).filter(Boolean);
  if (parts.length !== 5) return false;
  return parts.every((part) => /^[0-9*,/\-a-zA-Z?]+$/.test(part));
}

/** Whether two filter row lists carry the same content. */
export function sameFilters(a: EditFilterRow[], b: EditFilterRow[]): boolean {
  if (a.length !== b.length) return false;
  return a.every((row, i) => row.field === b[i]?.field && row.values === b[i]?.values);
}
