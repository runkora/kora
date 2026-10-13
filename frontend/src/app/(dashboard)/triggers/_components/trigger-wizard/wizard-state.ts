/**
 * @tier 3 — triggers-scoped. Pure state helpers shared by every kind: the step
 * sequence, the shared steps' validation, field-key derivation, and the draft
 * handed to the parent. No React, so every rule is unit-testable.
 */

import { resolveBrowserZone, timezoneOptions } from './cron';
import { LIMITS } from '../../_types';
import type { Senders, TriggerCoalesce } from '../../_types';
import type { SenderRestrictionDraft } from '../sender-restriction-editor';
import type { FilterOperator, FilterRow } from '../filter-rows-editor';
import type {
  KindStep,
  StepValidation,
  TriggerDraft,
  TriggerKind,
  TriggerKindDefinition,
  WizardField,
  WizardState,
} from './types';

export const VALID: StepValidation = { valid: true };

/**
 * Wraps a kind's own first-step checks with the two every kind shares: the
 * name before, the prompt after. The order is deliberate and preserved from
 * when each kind spelled it out — the hint tracks the top-to-bottom order of
 * the fields on screen, so the first unmet requirement the person sees is the
 * first one reported.
 */
export function validateNamedPromptStep(
  state: WizardState,
  inner: (state: WizardState) => StepValidation,
): StepValidation {
  if (!state.name.trim()) return { valid: false, hint: 'Give the trigger a name.' };
  const kind = inner(state);
  if (!kind.valid) return kind;
  if (!state.promptTemplate.trim())
    return { valid: false, hint: 'Write the prompt the run starts with.' };
  return VALID;
}

/**
 * Derives the API-visible variable key from a field's label. Keys must be
 * stable snake_case identifiers because callers hard-code them in request
 * bodies; the label is free text the console shows.
 */
export function deriveFieldKey(label: string, index: number): string {
  const slug = label
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '');
  return slug || `field_${index + 1}`;
}

/** A blank field row. */
export function newField(index: number): WizardField {
  return {
    id: `f_${index}_${Math.random().toString(36).slice(2, 8)}`,
    label: '',
    type: 'text',
    required: false,
  };
}

/** State shared by every kind, before the kind's own defaults are merged in. */
export function baseState(sources: { id: string }[]): WizardState {
  const browserZone = resolveBrowserZone();
  const zones = timezoneOptions(browserZone);
  return {
    name: '',
    promptTemplate: '',

    sourceChoice: sources[0]?.id ?? '',
    newSourceName: '',
    newSourceProvider: 'github',
    eventType: '',
    filters: [],
    senders: { logins: '', associations: [] },

    freq: 'daily',
    interval: '15',
    atMinute: '0',
    time: '02:00',
    weekdays: [1],
    dom: '1',
    customCron: '0 2 * * *',
    advancedOpen: false,
    timezone: browserZone && zones.includes(browserZone) ? browserZone : 'UTC',
    catchUp: false,

    fields: [newField(0)],

    agentTypeId: '',
    maxParallel: 4,
    overflow: 'wait',
    maxQueueDepth: 32,

    timeoutSeconds: 30,
    dedupeEnabled: true,
    autopauseEnabled: true,

    coalesceKey: '',
    coalesceWindow: '',
  };
}

/** Initial state for a kind: the shared defaults with the kind's own on top. */
export function initialState(def: TriggerKindDefinition, sources: { id: string }[]): WizardState {
  return { ...baseState(sources), ...def.defaults };
}

/** Splits a filter row's values on `|`, dropping blanks. */
export function splitFilterValues(values: string): string[] {
  return values
    .split('|')
    .map((v) => v.trim())
    .filter(Boolean);
}

/**
 * Folds the rows with one operator into the backend's field → alternatives map.
 *
 * Returns undefined when nothing is set: an empty object would read as a
 * condition that happens to be satisfied, where the trigger genuinely has none.
 * Repeating a field merges its values, since a clause is a set of alternatives.
 */
function conditionFrom(
  filters: FilterRow[],
  operator: FilterOperator,
): Record<string, string[]> | undefined {
  const condition: Record<string, string[]> = {};
  for (const row of filters) {
    if ((row.operator ?? 'is') !== operator) continue;
    const field = row.field.trim();
    const values = splitFilterValues(row.values);
    if (!field || values.length === 0) continue;
    condition[field] = [...(condition[field] ?? []), ...values];
  }
  return Object.keys(condition).length > 0 ? condition : undefined;
}

/** The `is` rows as the backend's `match_condition`. */
export function matchConditionFrom(filters: FilterRow[]): Record<string, string[]> | undefined {
  return conditionFrom(filters, 'is');
}

/** The `contains` rows as the backend's `match_contains`. */
export function matchContainsFrom(filters: FilterRow[]): Record<string, string[]> | undefined {
  return conditionFrom(filters, 'contains');
}

/**
 * A half-written filter is the one case worth blocking on: it looks configured
 * but silently does nothing, so the trigger would fire on deliveries the person
 * believes it excludes.
 */
export function validateFilters(filters: FilterRow[]): StepValidation {
  const halfWritten = filters.some((row) => {
    const hasField = Boolean(row.field.trim());
    const hasValues = splitFilterValues(row.values).length > 0;
    return hasField !== hasValues;
  });
  if (halfWritten) return { valid: false, hint: 'Finish or remove the empty filter' };
  return VALID;
}

/** Splits the logins box on commas and whitespace, dropping blanks and repeats. */
export function splitLogins(raw: string): string[] {
  return [...new Set(raw.split(/[\s,]+/).filter(Boolean))];
}

/** The sender restriction as the API takes it; undefined when nothing is set, which means anyone. */
export function senderRestrictionFrom(draft: SenderRestrictionDraft): Senders | undefined {
  const logins = splitLogins(draft.logins);
  const { associations } = draft;
  return logins.length > 0 || associations.length > 0 ? { logins, associations } : undefined;
}

/** Renders the composed condition for the review step. */
export function describeMatchCondition(filters: FilterRow[]): string | null {
  const parts = [
    ...Object.entries(matchConditionFrom(filters) ?? {}).map(
      ([field, values]) => `${field}=${values.join('|')}`,
    ),
    ...Object.entries(matchContainsFrom(filters) ?? {}).map(
      ([field, values]) => `${field} contains ${values.join('|')}`,
    ),
  ];
  return parts.length > 0 ? parts.join(' · ') : null;
}

/** The Target step: an agent, and limits the platform will accept. */
export function validateTarget(state: WizardState): StepValidation {
  if (!state.agentTypeId) return { valid: false, hint: 'Choose the agent this trigger runs.' };
  return VALID;
}

const textEncoder = new TextEncoder();

/**
 * Why a coalesce key and window cannot be sent, if anything. Both or neither,
 * the window within bounds, the key within its byte limit: the API's own rules,
 * so a bad value is caught here instead of as a 400.
 */
export function validateCoalesce(key: string, window: string): string | undefined {
  const k = key.trim();
  const w = window.trim();
  if (!k && !w) return undefined;
  if (!k) return 'Add a key, or clear the window.';
  if (!w) return 'Add a window in seconds, or clear the key.';
  const seconds = Number(w);
  const { min, max } = LIMITS.coalesce.window;
  if (!/^\d+$/.test(w) || seconds < min || seconds > max)
    return `Window must be a whole number from ${min} to ${max} seconds.`;
  if (textEncoder.encode(k).length > LIMITS.coalesce.keyBytes)
    return `Key must be at most ${LIMITS.coalesce.keyBytes} bytes.`;
  return undefined;
}

/** The coalesce setting as the API takes it; undefined when off or not valid. */
export function coalesceFrom(key: string, window: string): TriggerCoalesce | undefined {
  if (!key.trim() || validateCoalesce(key, window) !== undefined) return undefined;
  return { key: key.trim(), windowSeconds: Number(window.trim()) };
}

/**
 * The Delivery step: the three bounds on the queue, checked together because
 * they are configured together.
 */
export function validateDelivery(state: WizardState): StepValidation {
  if (state.maxParallel < LIMITS.parallel.min || state.maxParallel > LIMITS.parallel.max)
    return {
      valid: false,
      hint: `Max parallel runs must be between ${LIMITS.parallel.min} and ${LIMITS.parallel.max}.`,
    };
  if (
    state.overflow === 'wait' &&
    (state.maxQueueDepth < LIMITS.queueDepth.min || state.maxQueueDepth > LIMITS.queueDepth.max)
  )
    return {
      valid: false,
      hint: `Max queued deliveries must be between ${LIMITS.queueDepth.min} and ${LIMITS.queueDepth.max}.`,
    };
  if (state.timeoutSeconds < LIMITS.timeout.min || state.timeoutSeconds > LIMITS.timeout.max)
    return {
      valid: false,
      hint: `Timeout must be between ${LIMITS.timeout.min} and ${LIMITS.timeout.max} seconds.`,
    };
  // Coalescing needs `wait`; under any other overflow the section is hidden and ignored.
  const coalesce =
    state.overflow === 'wait'
      ? validateCoalesce(state.coalesceKey, state.coalesceWindow)
      : undefined;
  if (coalesce !== undefined) return { valid: false, hint: coalesce };
  return VALID;
}

/** The full step sequence for a kind: its own steps, then the shared three. */
export function stepsFor(def: TriggerKindDefinition, shared: KindStep[]): KindStep[] {
  return [...def.steps, ...shared];
}

/** The fields that carry a label, keyed for the API. */
export function resolvedFields(
  fields: WizardField[],
): { name: string; label: string; type: WizardField['type']; required: boolean }[] {
  return fields
    .filter((f) => f.label.trim())
    .map((f, i) => ({
      name: deriveFieldKey(f.label, i),
      label: f.label.trim(),
      type: f.type,
      required: f.required,
    }));
}

/** Example request body for the manual trigger's API preview. */
export function previewBody(fields: WizardField[]): string {
  const variables: Record<string, string> = {};
  for (const f of resolvedFields(fields)) {
    variables[f.name] =
      f.type === 'number'
        ? '0'
        : f.type === 'boolean'
          ? 'true'
          : f.type === 'choice'
            ? 'option'
            : '…';
  }
  return JSON.stringify({ variables }, null, 2);
}

/** Builds the draft the parent submits: shared fields plus the kind's own. */
export function toDraft(def: TriggerKindDefinition, state: WizardState): TriggerDraft {
  return {
    kind: def.id,
    name: state.name.trim(),
    promptTemplate: state.promptTemplate,
    agentTypeId: state.agentTypeId,
    maxParallel: state.maxParallel,
    overflow: state.overflow,
    maxQueueDepth: state.maxQueueDepth,
    timeoutSeconds: state.timeoutSeconds,
    dedupeEnabled: state.dedupeEnabled,
    autopauseEnabled: state.autopauseEnabled,
    ...def.draftPatch(state),
  };
}

/** The word beside the parallel-runs stepper. */
export function parallelWord(n: number): string {
  return n === 1 ? 'One at a time' : `Up to ${n} at once`;
}

/**
 * All three admission bounds in one sentence.
 *
 * Each overflow mode gets its own wording rather than a single template with
 * gaps: only `wait` has a queue to bound, so mentioning a queue depth under
 * `replace` or `refuse` would describe a limit that does not exist.
 *
 * Two claims are narrower than they look, and the wording says so. The timeout
 * bounds *starting* a run, never running it. And only a manual fire is refused
 * with a 429, because only a manual fire has a caller waiting on the response;
 * a webhook or scheduled arrival beyond the limit is recorded as dropped.
 */
export function admissionSummary(state: WizardState, kind: TriggerKind): string {
  const lead = `${parallelWord(state.maxParallel)}.`;
  const toStart = `up to ${state.timeoutSeconds}s to start`;
  if (state.overflow === 'wait') {
    return `${lead} Further deliveries wait in order, up to ${state.maxQueueDepth} of them, and each has ${toStart} once it is admitted.`;
  }
  if (state.overflow === 'replace') {
    return `${lead} While one runs, a newer delivery replaces the one waiting, and it has ${toStart}.`;
  }
  const refused =
    kind === 'manual' ? 'a fire is refused with 429' : 'further deliveries are recorded as dropped';
  return `${lead} Beyond that, ${refused}, and an admitted delivery has ${toStart}.`;
}
