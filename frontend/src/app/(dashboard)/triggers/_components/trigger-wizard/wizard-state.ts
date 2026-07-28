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

/** Field ids, shared so validation can point at the control that fixes it. */
export const FIELD_IDS = {
  name: 'wizard-name',
  prompt: 'wizard-prompt',
  source: 'wizard-source',
  sourceName: 'wizard-source-name',
  fields: 'wizard-fields',
  agent: 'wizard-agent',
  parallel: 'wizard-parallel',
  queueDepth: 'wizard-queue-depth',
  timeout: 'wizard-timeout',
};

/** The name every kind asks for on its first step. */
export function validateName(state: WizardState): StepValidation {
  if (!state.name.trim()) return { valid: false, blocker: 'Needs a name', fieldId: FIELD_IDS.name };
  return VALID;
}

/** The prompt every kind asks for on its first step. */
export function validatePrompt(state: WizardState): StepValidation {
  if (!state.promptTemplate.trim())
    return { valid: false, blocker: 'Needs a prompt', fieldId: FIELD_IDS.prompt };
  return VALID;
}

/** Returns the first failing check, so the blocker names one thing at a time. */
export function firstBlocker(...checks: (() => StepValidation)[]): StepValidation {
  for (const check of checks) {
    const result = check();
    if (!result.valid) return result;
  }
  return VALID;
}

/** The Target step: an agent, and limits the platform will accept. */
export function validateTarget(state: WizardState): StepValidation {
  if (!state.agentTypeId)
    return { valid: false, blocker: 'Needs an agent', fieldId: FIELD_IDS.agent };
  if (state.maxParallel < LIMITS.parallel.min || state.maxParallel > LIMITS.parallel.max)
    return {
      valid: false,
      blocker: `Needs 1 to ${LIMITS.parallel.max} parallel runs`,
      fieldId: FIELD_IDS.parallel,
    };
  if (
    state.overflow === 'wait' &&
    (state.maxQueueDepth < LIMITS.queueDepth.min || state.maxQueueDepth > LIMITS.queueDepth.max)
  )
    return {
      valid: false,
      blocker: `Needs a queue of ${LIMITS.queueDepth.min} to ${LIMITS.queueDepth.max}`,
      fieldId: FIELD_IDS.queueDepth,
    };
  return VALID;
}

/** The Delivery step. */
export function validateDelivery(state: WizardState): StepValidation {
  if (state.timeoutSeconds < LIMITS.timeout.min || state.timeoutSeconds > LIMITS.timeout.max)
    return {
      valid: false,
      blocker: `Needs a timeout of ${LIMITS.timeout.min} to ${LIMITS.timeout.max} seconds`,
      fieldId: FIELD_IDS.timeout,
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

/** The review rows every kind shows, after its own. Labels match the controls. */
export function sharedReviewRows(state: WizardState, context: WizardContext): ReviewRow[] {
  const agent = context.agents.find((a) => a.id === state.agentTypeId);
  return [
    {
      key: 'Agent to run',
      value: agent?.name?.trim() || agent?.slug || '—',
      description: 'Runs start as this agent and are checked against its grants.',
    },
    {
      key: 'Overlapping runs',
      value: overflowSentence(state),
      description:
        state.overflow === 'wait'
          ? `Deliveries beyond the limit wait in order, up to ${state.maxQueueDepth}.`
          : state.overflow === 'replace'
            ? 'Only the newest waiting delivery is kept.'
            : 'Deliveries beyond the limit are refused with 429.',
    },
    {
      key: 'Timeout',
      value: `${state.timeoutSeconds}s`,
      mono: true,
      description: 'Bounds starting the run, not running it.',
    },
    {
      key: 'Suppress duplicates',
      value: state.dedupeEnabled ? 'On' : 'Off',
      description: 'Ignore a replay of a delivery already processed.',
    },
    {
      key: 'Auto-pause on failure',
      value: state.autopauseEnabled ? 'On' : 'Off',
      description: 'Disable the trigger after ten consecutive failed deliveries.',
    },
  ];
}

/** The word beside the parallel-runs stepper. */
export function parallelWord(n: number): string {
  return n === 1 ? 'One at a time' : `Up to ${n} at once`;
}

/** Concurrency and overflow read as one sentence, the way they behave. */
export function overflowSentence(state: WizardState): string {
  return `${parallelWord(state.maxParallel)}, then ${state.overflow}`;
}
