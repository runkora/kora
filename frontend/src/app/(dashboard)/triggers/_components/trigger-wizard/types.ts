/**
 * @tier 3 — triggers-scoped. The wizard's public contract, its draft state, and
 * the registry contract every trigger kind implements.
 *
 * Trigger kinds are an enumerable, growing set: each answers "what wakes this
 * up" differently but shares everything downstream. So a kind is data — a
 * `TriggerKindDefinition` — and the wizard is a shell that renders whatever the
 * registry holds. Adding a kind is one file and one registry entry.
 */

import type * as React from 'react';
import type { IconName } from '@orbit/terra';
import type { TriggerKind, TriggerOverflow } from '@orbit/shared/api';
import type { Frequency } from './cron';

export type { TriggerKind, TriggerOverflow };

/** Field types a manual trigger may declare. Mirrors `InputFieldType`. */
export type WizardFieldType = 'text' | 'number' | 'choice' | 'boolean';

/** Sentinel for the "New source" option in the webhook source picker. */
export const NEW_SOURCE = '__new__';

export interface TriggerWizardAgent {
  id: string;
  slug: string;
  name?: string;
}

export interface TriggerWizardSource {
  id: string;
  name: string;
  provider: string;
}

/** What the parent needs in order to create the source and the trigger. */
export interface TriggerDraft {
  kind: TriggerKind;
  name: string;
  promptTemplate: string;
  source?: { existingId: string } | { newName: string; provider: string };
  /** Webhook only. Empty string means any event type. */
  eventType?: string;
  schedule?: { cronExpression: string; timezone: string; catchUp: boolean };
  manualInputs?: {
    name: string;
    label: string;
    type: WizardFieldType;
    required: boolean;
    choices?: string[];
  }[];
  agentTypeId: string;
  maxParallel: number;
  overflow: TriggerOverflow;
  maxQueueDepth: number;
  timeoutSeconds: number;
  dedupeEnabled: boolean;
  autopauseEnabled: boolean;
}

/** What the parent reports back once creation has been attempted. */
export type TriggerCreateResult =
  | { ok: true; triggerId: string; endpoint?: string; secret?: string }
  | { ok: false; error: string };

export interface TriggerWizardProps {
  /**
   * Opens straight into this kind's steps. Omit to open on the kind picker,
   * which is the first phase of the same modal.
   */
  initialKind?: TriggerKind;
  agents: TriggerWizardAgent[];
  sources: TriggerWizardSource[];
  /** Public base for webhook endpoints, e.g. "orbit.local.clearroute.io:8443". */
  ingestHost: string;
  /**
   * Executes creation. The parent creates the source first when requested and
   * then the trigger; resolves with what the done screen needs.
   */
  onCreate: (draft: TriggerDraft) => Promise<TriggerCreateResult>;
  /**
   * Opens the created trigger. When omitted the success screen offers only
   * "Create another" and "Done", so a host with nowhere to navigate does not
   * show a button that goes nowhere.
   */
  onViewTrigger?: (triggerId: string) => void;
  onClose: () => void;
}

/** One row of the manual trigger's field builder. */
export interface WizardField {
  /** Stable local id — the key is derived from the label and may collide mid-edit. */
  id: string;
  label: string;
  type: WizardFieldType;
  required: boolean;
}

/** Everything the wizard collects, in one object so steps stay stateless. */
export interface WizardState {
  name: string;
  promptTemplate: string;

  // ── Source (webhook) ──
  /** An existing source id, or `NEW_SOURCE`. */
  sourceChoice: string;
  newSourceName: string;
  newSourceProvider: string;
  /** Empty string means any event type. */
  eventType: string;

  // ── Schedule ──
  freq: Frequency;
  interval: string;
  atMinute: string;
  time: string;
  weekdays: number[];
  dom: string;
  customCron: string;
  advancedOpen: boolean;
  timezone: string;
  catchUp: boolean;

  // ── Inputs (manual) ──
  fields: WizardField[];

  // ── Target ──
  agentTypeId: string;
  maxParallel: number;
  overflow: TriggerOverflow;
  maxQueueDepth: number;

  // ── Delivery ──
  timeoutSeconds: number;
  dedupeEnabled: boolean;
  autopauseEnabled: boolean;
}

/** The tenant-scoped data a step needs but does not own. */
export interface WizardContext {
  agents: TriggerWizardAgent[];
  sources: TriggerWizardSource[];
  ingestHost: string;
}

/**
 * Whether a step is complete enough to move on, and what is missing if not.
 *
 * Continue is never disabled — pressing it on an incomplete step is how a
 * person asks what is missing, so the blocker names the one thing to fix and
 * points at the control that fixes it.
 */
export interface StepValidation {
  valid: boolean;
  /** Blocker fragment, e.g. "Needs a name". Absent when the step is complete. */
  blocker?: string;
  /** `id` of the control to focus and mark, e.g. "wizard-name". */
  fieldId?: string;
}

export interface StepComponentProps {
  state: WizardState;
  patch: (next: Partial<WizardState>) => void;
  context: WizardContext;
  /**
   * The kind being created. Shared steps read it (the review step asks the
   * kind to summarise itself); a kind's own steps rarely need it.
   */
  definition: TriggerKindDefinition;
  /** The last failed create, kept on screen without losing any state. */
  error: string | null;
  /**
   * The blocker for a control, once the person has pressed Continue on an
   * incomplete step. Returns undefined until then, so a half-typed form is
   * never scolded mid-keystroke.
   */
  fieldError: (fieldId: string) => string | undefined;
  /** Attach to the step's first control so focus lands there on arrival. */
  firstFieldRef: React.Ref<HTMLInputElement>;
}

/** One step in the wizard: a rail entry and the pane it shows. */
export interface KindStep {
  id: string;
  /** Rail label. Titles only; the rail carries no subtitles. */
  railTitle: string;
  /** The sentence under the step heading. Omitted when the controls say it. */
  description?: string;
  Component: React.ComponentType<StepComponentProps>;
  validate: (state: WizardState, context: WizardContext) => StepValidation;
}

/** One key/value line on the review step. */
export interface ReviewRow {
  /** Must be the exact label of the control that set it. */
  key: string;
  value: string;
  /** What the setting does, in the words the control used. */
  description?: string;
  /** Code-ish values read as data, not prose. */
  mono?: boolean;
}

export interface DoneContentProps {
  state: WizardState;
  context: WizardContext;
  triggerId: string;
  /** Full inbound URL, when the kind has one. */
  endpoint?: string;
  /** One-time signing secret, when creation minted one. */
  secret?: string;
}

/**
 * Everything the wizard needs to know about one kind of trigger. The core
 * wizard reads only this; it never branches on `id`.
 */
export interface TriggerKindDefinition {
  id: TriggerKind;
  icon: IconName;
  title: string;
  description: string;
  /** The mono usage hint under the picker card, e.g. "GitHub · Alertmanager". */
  example: string;
  /** State this kind starts from, merged over the shared defaults. */
  defaults?: Partial<WizardState>;
  /** The kind's own configuration steps, before the shared target/delivery/review. */
  steps: KindStep[];
  /** The kind's rows on the review step, above the shared ones. */
  summarize: (state: WizardState, context: WizardContext) => ReviewRow[];
  /** The kind's own fields on the draft sent to the parent. */
  draftPatch: (state: WizardState) => Partial<TriggerDraft>;
  /** The kind-specific block on the success screen. */
  doneContent: React.ComponentType<DoneContentProps>;
}

/**
 * A kind the model accommodates but that is not built.
 *
 * These are shown, disabled, rather than hidden. It tells an admin where the
 * product is going without promising a date, and it keeps the design honest:
 * if adding one would require reworking the delivery model, the model is wrong
 * now rather than later.
 */
export interface PlannedKind {
  icon: IconName;
  title: string;
  description: string;
}
