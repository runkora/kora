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
import type { FieldCatalogEntry } from './trigger-wizard/types';

/**
 * Common event types per provider, mirroring the wizard's source step so a
 * trigger offers the same vocabulary whether it is being created or corrected.
 */
export const PROVIDER_EVENTS: Record<string, string[]> = {
  github: ['push', 'pull_request', 'issues', 'release', 'workflow_run'],
  alertmanager: ['firing', 'resolved'],
};

/**
 * The events to offer for a provider, including the trigger's current one.
 *
 * A trigger may listen on an event outside the common list: the backend takes
 * whatever the provider sends. Dropping it from the control would silently
 * rewrite the trigger the moment anything else on the row was saved.
 */
export function eventOptionsFor(provider: string, current: string): string[] {
  const common = PROVIDER_EVENTS[provider] ?? [];
  return current && !common.includes(current) ? [...common, current] : common;
}

/** One editable filter row. Values stay a raw string while being typed. */
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
  return (inputs ?? []).map((input) => ({
    token: `var.${input.name}`,
    description: input.label?.trim()
      ? `The "${input.label.trim()}" input`
      : `The ${input.name} input`,
    example:
      input.type === 'number'
        ? '12'
        : input.type === 'boolean'
          ? 'true'
          : (input.choices?.[0] ?? input.default ?? 'a value'),
    group: 'Inputs',
  }));
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
