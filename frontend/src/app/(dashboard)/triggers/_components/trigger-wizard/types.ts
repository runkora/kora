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
import type { IconName } from '@runkora/hades';
import type {
  TriggerKind,
  TriggerOverflow,
  TriggerRoutingConfidence,
  TriggerTargetKind,
} from '@runkora/shared/api';
import type { Frequency } from './cron';
import type { FilterRow } from '../filter-rows-editor';
import type { SenderRestrictionDraft } from '../sender-restriction-editor';
import type { Senders, TriggerCoalesce, TriggerRoutingRow } from '../../_types';

export type { TriggerKind, TriggerOverflow };

/** Field types a manual trigger may declare. Mirrors `InputFieldType`. */
export type WizardFieldType = 'text' | 'number' | 'choice' | 'boolean';

/** Sentinel for the "New source" option in the webhook source picker. */
export const NEW_SOURCE = '__new__';

export interface TriggerWizardAgent {
  id: string;
  slug: string;
  name?: string;
  /**
   * Delegated agents run on behalf of a person, so an automated trigger can
   * never bind one; the picker shows them disabled with the reason rather
   * than hiding them, so the list matches the agents page.
   */
  identityMode?: 'autonomous' | 'delegated';
  /** False when the caller may not run the agent, which binding a trigger needs. Absent reads as allowed. */
  canRun?: boolean;
}

/**
 * An agent as the routing step lists it. The description is what kora routes
 * on; `reason` says why an agent that is not runnable cannot be a candidate.
 */
export interface RoutingAgent extends TriggerWizardAgent {
  description: string;
  runnable: boolean;
  reason?: string;
}

/** A model the decision model picker offers. */
export interface DecisionModel {
  id: string;
  name: string;
}

/** A list the wizard loads while it is open. */
export type LoadedList<T> =
  { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; items: T[] };

/** What "Let kora choose" needs: the candidate agents and the decision models. */
export interface RoutingOptions {
  candidates: LoadedList<RoutingAgent>;
  models: LoadedList<DecisionModel>;
}

/** Route every delivery to one agent, or let kora choose the agent per delivery. */
export type RoutingMode = 'one' | 'choose';

/** What happens to a delivery kora is unsure about. */
export type UnsureAction = 'fallback' | 'none';

/** The routing settings as the Target step edits them. */
export interface RoutingDraft {
  mode: RoutingMode;
  candidateIds: string[];
  modelId: string;
  confidence: TriggerRoutingConfidence;
  unsure: UnsureAction;
  fallbackId: string;
  joinOpenRuns: boolean;
  /** Template rendered per delivery; a match joins that open run with no model call. */
  joinKey: string;
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
  /** `signingSecret` is set for a provider that issues its own secret, e.g. Slack. */
  source?: { existingId: string } | { newName: string; provider: string; signingSecret?: string };
  /** Webhook only. */
  eventType?: string;
  /**
   * Webhook only. field → allowed values; a delivery matches when every clause
   * is satisfied and any one value in a clause matches. Omitted when no filter
   * is set, which matches every delivery of the chosen event.
   */
  matchCondition?: Record<string, string[]>;
  /** Webhook only. field → substrings, matched ignoring case; omitted when unset. */
  matchContains?: Record<string, string[]>;
  /** Webhook only. Omitted when anyone may fire the trigger. */
  senderRestriction?: Senders;
  /** Webhook only. Omitted unless both fields are set and overflow is `wait`. */
  coalesce?: TriggerCoalesce;
  schedule?: { cronExpression: string; timezone: string; catchUp: boolean };
  manualInputs?: {
    name: string;
    label: string;
    type: WizardFieldType;
    required: boolean;
    choices?: string[];
  }[];
  /** "routed" sends `routing` and leaves `agentTypeId` empty. */
  targetKind: TriggerTargetKind;
  agentTypeId: string;
  /** Set when `targetKind` is "routed". */
  routing?: TriggerRoutingRow;
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
  /** Why the agents could not be read; the target step says so instead of offering none. */
  agentsError?: string | null;
  sources: TriggerWizardSource[];
  /** Public base for webhook endpoints, e.g. "api.orbit.local.clearroute.io:8443". */
  ingestHost: string;
  /**
   * Offers "Let kora choose" on a webhook trigger's Target step. Off on a
   * surface that creates triggers for one agent.
   */
  allowRouting?: boolean;
  /**
   * Executes creation. The parent creates the source first when requested and
   * then the trigger; resolves with what the done screen needs.
   */
  onCreate: (draft: TriggerDraft) => Promise<TriggerCreateResult>;
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
  /** The signing secret the provider issued, for a new source whose provider issues it. */
  newSourceSecret: string;
  eventType: string;
  /** Match-condition rows. Empty means every delivery of the event qualifies. */
  filters: FilterRow[];
  senders: SenderRestrictionDraft;

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
  routing: RoutingDraft;
  maxParallel: number;
  overflow: TriggerOverflow;
  maxQueueDepth: number;

  // ── Delivery ──
  timeoutSeconds: number;
  dedupeEnabled: boolean;
  autopauseEnabled: boolean;

  // ── Coalescing (webhook, overflow `wait`) ──
  coalesceEnabled: boolean;
  /** Key template, raw while typed. */
  coalesceKey: string;
  /** Window in seconds, raw while typed. */
  coalesceWindow: string;
}

/** The tenant-scoped data a step needs but does not own. */
export interface WizardContext {
  agents: TriggerWizardAgent[];
  agentsError?: string | null;
  sources: TriggerWizardSource[];
  ingestHost: string;
  /** Present when the Target step offers "Let kora choose". */
  routing?: RoutingOptions;
}

/** Whether a step is complete enough to move on, and what is missing if not. */
export interface StepValidation {
  valid: boolean;
  /** Shown beside the disabled Continue button. Absent when the step is complete. */
  hint?: string;
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
  /** Jumps to another step by id; the review step's Edit buttons use it. */
  onGoTo?: (stepId: string) => void;
}

/**
 * Copy that may name the product: a string, or a function of the product name.
 * The registry is module-level, so the rendering component supplies the name.
 */
export type BrandedCopy = string | ((productName: string) => string);

export function brandedCopy(copy: BrandedCopy, productName: string): string {
  return typeof copy === 'function' ? copy(productName) : copy;
}

/** One step in the wizard: a rail entry and the pane it shows. */
export interface KindStep {
  id: string;
  railTitle: string;
  railSub: string;
  /** The sentence under the step heading. */
  description: BrandedCopy;
  Component: React.ComponentType<StepComponentProps>;
  validate: (state: WizardState, context: WizardContext) => StepValidation;
}

/** One key/value line on the review step. */
export interface ReviewRow {
  key: string;
  value: string;
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

/** One `var.` value a scheduled or manual fire supplies to the prompt. */
export interface FieldCatalogEntry {
  /** Dotted token, e.g. `var.date`. */
  token: string;
  /** One line, in the words a person would use. */
  description: string;
  /** A plausible sample value, rendered by the prompt preview. */
  example?: string;
  /** Picker group heading, e.g. "Schedule". Defaults from the token prefix. */
  group?: string;
}

/**
 * Everything the wizard needs to know about one kind of trigger. The core
 * wizard reads only this; it never branches on `id`.
 */
export interface TriggerKindDefinition {
  id: TriggerKind;
  icon: IconName;
  title: string;
  description: BrandedCopy;
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
  /**
   * The delivery fields this kind's runs can read, for the prompt builder and
   * (where the kind supports it) the filter picker. Omitted when a kind carries
   * no fields at all.
   */
  fieldCatalog?: (state: WizardState, context: WizardContext) => FieldCatalogEntry[];
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
  description: BrandedCopy;
}
