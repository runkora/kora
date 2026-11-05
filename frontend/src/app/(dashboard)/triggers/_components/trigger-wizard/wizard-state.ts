/**
 * @tier 3 — triggers-scoped. Pure state helpers shared by every kind: the step
 * sequence, the shared steps' validation, field-key derivation, and the draft
 * handed to the parent. No React, so every rule is unit-testable.
 */

import type { ModelObject, TriggerRoutingCandidate } from '@runkora/shared/api';
import { resolveBrowserZone, timezoneOptions } from './cron';
import { LIMITS } from '../../_types';
import { firstUnknownChatField, placeholderKeys } from '../../_lib/chat';
import { EMPTY_GENERIC_DRAFT, fixedEventType } from '../../_lib/generic-source';
import { FIELD_PATH } from '../../_lib/field-path';
import type { Senders, TriggerCoalesce, TriggerRoutingRow } from '../../_types';
import type { SenderRestrictionDraft } from '../sender-restriction-editor';
import type { FilterOperator, FilterRow } from '../filter-rows-editor';
import type {
  DecisionModel,
  KindStep,
  RoutingAgent,
  RoutingDraft,
  RoutingOptions,
  StepValidation,
  TriggerDraft,
  TriggerKind,
  TriggerKindDefinition,
  TriggerWizardSource,
  WizardContext,
  WizardField,
  WizardState,
} from './types';
import { NEW_SOURCE } from './types';

export const VALID: StepValidation = { valid: true };

const textEncoder = new TextEncoder();

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
export function baseState(sources: readonly { id: string }[]): WizardState {
  const browserZone = resolveBrowserZone();
  const zones = timezoneOptions(browserZone);
  return {
    name: '',
    promptTemplate: '',

    sourceChoice: sources[0]?.id ?? '',
    newSourceName: '',
    newSourceProvider: 'github',
    newSourceSecret: '',
    newSourceCreated: null,
    newSourceSecretSaved: false,
    newSourceGeneric: EMPTY_GENERIC_DRAFT,
    eventType: '',
    eventFields: null,
    eventFieldsFailed: false,
    filters: [],
    senders: { ids: '', roles: [] },

    freq: 'daily',
    interval: '15',
    atMinute: '0',
    time: '02:00',
    weekdays: [1],
    dom: '1',
    customCron: '0 2 * * *',
    timezone: browserZone && zones.includes(browserZone) ? browserZone : 'UTC',
    catchUp: false,

    fields: [newField(0)],

    agentTypeId: '',
    routing: DEFAULT_ROUTING,
    maxParallel: 4,
    overflow: 'wait',
    maxQueueDepth: 32,

    timeoutSeconds: 30,
    dedupeEnabled: true,
    autopauseEnabled: true,

    coalesceEnabled: false,
    coalesceKey: '',
    coalesceWindow: '',
  };
}

/**
 * Initial state for a kind: the shared defaults with the kind's own on top. A
 * kind that lists sources of one category starts on the first of those.
 */
export function initialState(
  def: TriggerKindDefinition,
  sources: readonly TriggerWizardSource[],
): WizardState {
  const own =
    def.sourceCategory === undefined
      ? sources
      : sources.filter((s) => s.category === def.sourceCategory);
  const state = { ...baseState(own), ...def.defaults };
  if (own.length === 0 && def.newSourceWhenNone) return { ...state, sourceChoice: NEW_SOURCE };
  // A generic source with no event type location gives every delivery one event type.
  const first = own[0];
  const eventType = first === undefined ? undefined : fixedEventType(first.provider, first.config);
  return eventType === undefined ? state : { ...state, eventType };
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
 * Repeating a field merges its values, dropping repeats, since a clause is a set of alternatives.
 */
function conditionFrom(
  filters: FilterRow[],
  operator: FilterOperator,
): Record<string, string[]> | undefined {
  // A Map, so a field named like an Object.prototype member (`__proto__`) is only a key.
  const condition = new Map<string, string[]>();
  for (const row of filters) {
    if ((row.operator ?? 'is') !== operator) continue;
    const field = row.field.trim();
    const values = splitFilterValues(row.values);
    if (!field || values.length === 0) continue;
    condition.set(field, [...new Set([...(condition.get(field) ?? []), ...values])]);
  }
  return condition.size > 0 ? Object.fromEntries(condition) : undefined;
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
  // A typed field path must parse; a field from the schema always does.
  const malformed = filters.find((row) => row.field.trim() && !FIELD_PATH.test(row.field.trim()));
  if (malformed)
    return {
      valid: false,
      hint: `${malformed.field.trim()} is not a field path. Use dotted names, such as data.issue.title.`,
    };
  return VALID;
}

/** Splits the sender ids box on commas and whitespace, dropping blanks and repeats. */
export function splitIds(raw: string): string[] {
  return [...new Set(raw.split(/[\s,]+/).filter(Boolean))];
}

/** The sender restriction as the API takes it; undefined when nothing is set, which means anyone. */
export function senderRestrictionFrom(draft: SenderRestrictionDraft): Senders | undefined {
  const ids = splitIds(draft.ids);
  const { roles } = draft;
  return ids.length > 0 || roles.length > 0 ? { ids, roles } : undefined;
}

/** The hint for a placeholder or filter field the trigger cannot read. */
export function unknownFieldHint(key: string): string {
  return `${key} is not a field of this message. Use one listed under Insert field.`;
}

/** The hint for a join or coalesce key placeholder; those inputs have no Insert field picker. */
export function unknownKeyFieldHint(key: string): string {
  return `${key} is not a field of this message. Use a chat field such as {{ conversation.id }}.`;
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

export const DEFAULT_ROUTING: RoutingDraft = {
  mode: 'one',
  candidateIds: [],
  modelId: '',
  confidence: 'balanced',
  unsure: 'fallback',
  fallbackId: '',
  joinOpenRuns: true,
  joinKey: '',
};

/** The tenant catalog narrowed to the models that can answer a routing question. */
export function decisionModelsOf(models: ModelObject[]): DecisionModel[] {
  // The gateway omits capabilities for a model it has no discovered route data for.
  return models
    .filter((m) => m.status === 'available' && (m.capabilities ?? []).includes('decision'))
    .map((m) => ({ id: m.id, name: m.display_name || m.id }));
}

/** A routing candidate from the API, as the picker lists it. */
export function toRoutingAgent(c: TriggerRoutingCandidate): RoutingAgent {
  return {
    id: c.id,
    slug: c.slug,
    name: c.name,
    description: c.description,
    identityMode: c.identity_mode ?? 'autonomous',
    runnable: c.runnable,
    ...(c.reason !== undefined && c.reason !== '' && { reason: c.reason }),
  };
}

/** Below this many characters a description is too thin to route on. */
const MIN_DESCRIPTION = 40;

export function needsDescription(agent: RoutingAgent): boolean {
  return agent.description.trim().length < MIN_DESCRIPTION;
}

/** Whether the Target step is set to let kora choose. */
export function isRouted(state: WizardState, context: WizardContext): boolean {
  return context.routing !== undefined && state.routing.mode === 'choose';
}

/** No decision model in the catalog: the mode cannot be completed. */
export function noDecisionModels(options: RoutingOptions): boolean {
  return options.models.status === 'ready' && options.models.items.length === 0;
}

export function validateRouting(draft: RoutingDraft, options: RoutingOptions): StepValidation {
  // The step says why inline and hides everything else, so no hint repeats it.
  if (noDecisionModels(options)) return { valid: false };
  if (draft.candidateIds.length < 1)
    return { valid: false, hint: 'Pick at least one candidate agent.' };
  if (draft.modelId === '') return { valid: false, hint: 'Choose a decision model.' };
  if (draft.unsure === 'fallback' && !draft.candidateIds.includes(draft.fallbackId))
    return { valid: false, hint: 'Choose the fallback agent.' };
  if (draft.joinOpenRuns && textEncoder.encode(draft.joinKey.trim()).length > LIMITS.joinKeyBytes)
    return { valid: false, hint: `Join key must be at most ${LIMITS.joinKeyBytes} bytes.` };
  return VALID;
}

/** The Target step: one agent, or the routing settings when kora chooses. */
export function validateTarget(state: WizardState, context?: WizardContext): StepValidation {
  if (context?.routing !== undefined && state.routing.mode === 'choose') {
    const routing = validateRouting(state.routing, context.routing);
    // eventFields is set only for a chat command, whose backend refuses a join key outside its fields.
    if (!routing.valid || !state.routing.joinOpenRuns || state.eventFields === null) return routing;
    const unknown = firstUnknownChatField(
      placeholderKeys(state.routing.joinKey),
      state.eventFields,
    );
    return unknown === undefined ? VALID : { valid: false, hint: unknownKeyFieldHint(unknown) };
  }
  if (!state.agentTypeId) return { valid: false, hint: 'Choose the agent this trigger runs.' };
  return VALID;
}

/** The routing settings as the trigger stores them. */
export function routingRowOf(draft: RoutingDraft): TriggerRoutingRow {
  return {
    candidates: draft.candidateIds,
    model: draft.modelId,
    confidence: draft.confidence,
    fallbackAgentTypeId: draft.unsure === 'fallback' ? draft.fallbackId : null,
    joinRuns: draft.joinOpenRuns,
    joinKey: draft.joinOpenRuns ? draft.joinKey.trim() : '',
  };
}

/** The coalesce setting as edited: a switch, and the raw key and window. */
export interface CoalesceDraft {
  enabled: boolean;
  key: string;
  window: string;
}

/** The wizard's coalesce fields as one draft. */
export function coalesceDraftOf(state: WizardState): CoalesceDraft {
  return { enabled: state.coalesceEnabled, key: state.coalesceKey, window: state.coalesceWindow };
}

/**
 * What is wrong with each coalesce field, if anything. These are the API's own
 * rules, so a bad value is caught here instead of as a 400. Off has no errors.
 */
export function coalesceErrors(c: CoalesceDraft): { key?: string; window?: string } {
  if (!c.enabled) return {};
  const k = c.key.trim();
  const w = c.window.trim();
  const { min, max } = LIMITS.coalesce.window;
  const errors: { key?: string; window?: string } = {};
  if (!k) errors.key = 'Add a key template.';
  else if (textEncoder.encode(k).length > LIMITS.coalesce.keyBytes)
    errors.key = `Key must be at most ${LIMITS.coalesce.keyBytes} bytes.`;
  if (!w) errors.window = 'Add a window in seconds.';
  else if (!/^\d+$/.test(w) || Number(w) < min || Number(w) > max)
    errors.window = `Window must be a whole number from ${min} to ${max} seconds.`;
  return errors;
}

/** The first reason the coalesce setting cannot be sent, if any. */
export function validateCoalesce(c: CoalesceDraft): string | undefined {
  const errors = coalesceErrors(c);
  return errors.key ?? errors.window;
}

/** The coalesce setting as the API takes it; undefined when off or not valid. */
export function coalesceFrom(c: CoalesceDraft): TriggerCoalesce | undefined {
  if (!c.enabled || validateCoalesce(c) !== undefined) return undefined;
  return { key: c.key.trim(), windowSeconds: Number(c.window.trim()) };
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
    !(state.maxQueueDepth >= LIMITS.queueDepth.min && state.maxQueueDepth <= LIMITS.queueDepth.max)
  )
    return {
      valid: false,
      hint: `Max queued deliveries must be between ${LIMITS.queueDepth.min} and ${LIMITS.queueDepth.max}.`,
    };
  if (!(state.timeoutSeconds >= LIMITS.timeout.min && state.timeoutSeconds <= LIMITS.timeout.max))
    return {
      valid: false,
      hint: `Timeout must be between ${LIMITS.timeout.min} and ${LIMITS.timeout.max} seconds.`,
    };
  // Coalescing needs `wait`; under any other overflow the section is hidden and ignored.
  const coalesce = state.overflow === 'wait' ? validateCoalesce(coalesceDraftOf(state)) : undefined;
  if (coalesce !== undefined) return { valid: false, hint: coalesce };
  // eventFields is set only for a kind whose backend refuses unknown fields,
  // so a bad key is caught here rather than after a new source exists.
  if (state.overflow === 'wait' && state.coalesceEnabled && state.eventFields !== null) {
    const unknown = firstUnknownChatField(placeholderKeys(state.coalesceKey), state.eventFields);
    if (unknown !== undefined) return { valid: false, hint: unknownKeyFieldHint(unknown) };
  }
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
  // Only a webhook trigger can be routed, a chat command included; the step offers the mode for no other kind.
  const routed = def.apiKind === 'webhook' && state.routing.mode === 'choose';
  return {
    kind: def.apiKind,
    name: state.name.trim(),
    promptTemplate: state.promptTemplate,
    ...(routed
      ? { targetKind: 'routed' as const, agentTypeId: '', routing: routingRowOf(state.routing) }
      : { targetKind: 'agent' as const, agentTypeId: state.agentTypeId }),
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

/** A run of the admission summary; `strong` marks the numbers and outcomes worth scanning for. */
export interface SummarySegment {
  text: string;
  strong?: boolean;
}

/** The summary as plain text, for places that cannot bold. */
export function summaryText(segments: SummarySegment[]): string {
  return segments.map((s) => s.text).join('');
}

/**
 * All three admission bounds in one sentence, as segments so the figures can be bold.
 *
 * Each overflow mode gets its own wording rather than a single template with
 * gaps: only `wait` has a queue to bound, so mentioning a queue depth under
 * `replace` or `refuse` would describe a limit that does not exist.
 *
 * Three claims are narrower than they look, and the wording says so. The timeout
 * bounds *starting* a run, never running it. Under `refuse` the ceiling counts
 * deliveries still waiting to start, not only running ones. And only a manual
 * fire is refused with a 429, because only a manual fire has a caller waiting on
 * the response; a webhook or scheduled arrival beyond the limit is recorded as dropped.
 */
export function admissionSummary(
  state: Pick<WizardState, 'maxParallel' | 'timeoutSeconds' | 'overflow' | 'maxQueueDepth'>,
  kind: TriggerKind,
): SummarySegment[] {
  const n = state.maxParallel;
  const timeout = { text: `${state.timeoutSeconds} seconds`, strong: true };
  const count = { text: String(n), strong: true };
  if (state.overflow === 'wait') {
    return [
      ...(n === 1
        ? [count, { text: ' run at a time. Up to ' }]
        : [{ text: 'Up to ' }, count, { text: ' run at once. Up to ' }]),
      { text: String(state.maxQueueDepth), strong: true },
      { text: ' more wait in order, and each has ' },
      timeout,
      { text: ' to start once admitted.' },
    ];
  }
  if (state.overflow === 'replace') {
    return [
      ...(n === 1
        ? [count, { text: " run at a time. While it's busy, one event waits and " }]
        : [
            { text: 'Up to ' },
            count,
            { text: " run at once. While they're busy, one event waits and " },
          ]),
      { text: 'a newer one replaces it', strong: true },
      { text: '. Each admitted event has ' },
      timeout,
      { text: ' to start.' },
    ];
  }
  const manual = kind === 'manual';
  return [
    ...(n === 1
      ? [count, { text: ' at a time' }]
      : [{ text: 'Up to ' }, count, { text: ' at once' }]),
    { text: ', counting any still waiting to start. Any more ' },
    { text: manual ? 'fires are ' : 'are ' },
    manual
      ? { text: 'refused with an error (429)', strong: true }
      : { text: 'turned away', strong: true },
    { text: manual ? '. Each has ' : ' and recorded as dropped. Each has ' },
    timeout,
    { text: ' to start.' },
  ];
}
