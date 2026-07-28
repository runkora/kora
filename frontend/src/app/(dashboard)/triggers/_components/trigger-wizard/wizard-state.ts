/**
 * @tier 3 — triggers-scoped. Pure state helpers shared by every kind: the step
 * sequence, the shared steps' validation, field-key derivation, and the draft
 * handed to the parent. No React, so every rule is unit-testable.
 */

import { resolveBrowserZone, timezoneOptions } from './cron';
import type {
  KindStep,
  ReviewRow,
  StepValidation,
  TriggerDraft,
  TriggerKindDefinition,
  WizardContext,
  WizardField,
  WizardState,
} from './types';

export const VALID: StepValidation = { valid: true };

/** Bounds the platform enforces, mirrored here so the UI refuses before the API does. */
export const LIMITS = {
  parallel: { min: 1, max: 32 },
  queueDepth: { min: 1, max: 500 },
  timeout: { min: 5, max: 600 },
};

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
  };
}

/** Initial state for a kind: the shared defaults with the kind's own on top. */
export function initialState(def: TriggerKindDefinition, sources: { id: string }[]): WizardState {
  return { ...baseState(sources), ...def.defaults };
}

/** Trigger name and prompt are asked for on every kind's first step. */
export function validateIdentity(state: WizardState): StepValidation {
  if (!state.name.trim()) return { valid: false, hint: 'Give the trigger a name.' };
  if (!state.promptTemplate.trim())
    return { valid: false, hint: 'Write the prompt the run starts with.' };
  return VALID;
}

/** The Target step: an agent, and limits the platform will accept. */
export function validateTarget(state: WizardState): StepValidation {
  if (!state.agentTypeId) return { valid: false, hint: 'Choose the agent this trigger runs.' };
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
  return VALID;
}

/** The Delivery step. */
export function validateDelivery(state: WizardState): StepValidation {
  if (state.timeoutSeconds < LIMITS.timeout.min || state.timeoutSeconds > LIMITS.timeout.max)
    return {
      valid: false,
      hint: `Timeout must be between ${LIMITS.timeout.min} and ${LIMITS.timeout.max} seconds.`,
    };
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

/** The review rows every kind shows, after its own. */
export function sharedReviewRows(state: WizardState, context: WizardContext): ReviewRow[] {
  const agent = context.agents.find((a) => a.id === state.agentTypeId);
  return [
    { key: 'Agent', value: agent?.name?.trim() || agent?.slug || '—' },
    { key: 'Concurrency', value: `${state.maxParallel} parallel · ${state.overflow}` },
    { key: 'Timeout', value: `${state.timeoutSeconds}s`, mono: true },
    { key: 'Suppress duplicates', value: state.dedupeEnabled ? 'On' : 'Off' },
    { key: 'Auto-pause', value: state.autopauseEnabled ? 'On' : 'Off' },
  ];
}

/** The word beside the parallel-runs stepper. */
export function parallelWord(n: number): string {
  return n === 1 ? 'One at a time' : `Up to ${n} at once`;
}
