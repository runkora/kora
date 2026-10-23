/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * and the agent detail page (`agents/[slug]/_components/agent-triggers-tab.tsx`),
 * which imports it across feature areas. Stays Tier 3 deliberately: this file IS
 * the Triggers vocabulary, and promoting it would move the whole domain into
 * `src/components/` for the sake of one consumer.
 *
 * Display-layer types for the Triggers feature.
 *
 * *Row types are the shapes consumed by the UI. The raw API shapes
 * (WebhookSource, Trigger, TriggerDelivery) only appear at the service boundary.
 */

import type { IconName } from '@runkora/hades';
import type {
  InputField,
  SenderRestriction,
  SourceCategory,
  Trigger,
  TriggerCoalesce as TriggerCoalesceWire,
  TriggerDelivery,
  TriggerKind,
  TriggerOverflow,
  TriggerRouteOutcome,
  TriggerRouting,
  TriggerRoutingConfidence,
  TriggerRoutingDecision,
  TriggerTargetKind,
  TriggerUpdateRequest,
  WebhookSource,
} from '@runkora/shared/api';

export type {
  InputField,
  SourceCategory,
  TriggerKind,
  TriggerOverflow,
  TriggerRouteOutcome,
  TriggerRoutingConfidence,
  TriggerTargetKind,
};

/** Burst coalescing for a webhook trigger: a key template and a window in seconds. */
export interface TriggerCoalesce {
  key: string;
  windowSeconds: number;
}

/** Who may fire a webhook or chat command trigger; both lists empty means anyone. */
export type Senders = Required<SenderRestriction>;

/** A routed trigger's candidates and settings. */
export interface TriggerRoutingRow {
  /** Agent type ids. */
  candidates: string[];
  /** The decision model's catalog id. */
  model: string;
  confidence: TriggerRoutingConfidence;
  /** Null starts no run when routing is unsure. */
  fallbackAgentTypeId: string | null;
  joinRuns: boolean;
  /** Empty when the trigger has no join key. */
  joinKey: string;
}

/**
 * The probability each confidence preset routes at. Mirrors the backend's
 * thresholds, so the delivery panel can name the preset a decision used.
 */
export const CONFIDENCE_THRESHOLDS: Record<TriggerRoutingConfidence, number> = {
  strict: 0.85,
  balanced: 0.7,
  loose: 0.5,
};

/** The preset a recorded threshold belongs to, or null for a value none of them uses. */
export function confidencePresetOf(threshold: number): TriggerRoutingConfidence | null {
  const found = (Object.keys(CONFIDENCE_THRESHOLDS) as TriggerRoutingConfidence[]).find(
    (p) => Math.abs(CONFIDENCE_THRESHOLDS[p] - threshold) < 1e-9,
  );
  return found ?? null;
}

/**
 * How the console groups triggers. A chat command trigger is stored as a
 * `webhook` trigger on a chat source, so `chat` exists only in the console.
 */
export type ConsoleKind = TriggerKind | 'chat';

/**
 * A trigger's console kind: `chat` for a webhook trigger whose source is a
 * chat source, else its stored kind.
 */
export function consoleKindOf(
  t: Pick<TriggerRow, 'kind' | 'sourceId'>,
  sourceCategory: (sourceId: string) => SourceCategory | undefined,
): ConsoleKind {
  return t.kind === 'webhook' && sourceCategory(t.sourceId) === 'chat' ? 'chat' : t.kind;
}

// ── Shared vocabulary ─────────────────────────────────────────────────────────
//
// Constants both the create surface (wizard) and the read/edit surfaces (list,
// drawer) need. They live here rather than in either one, so a bound or an icon
// cannot mean two things depending on which screen you are looking at.

/**
 * Bounds the platform enforces, mirrored here so the UI refuses before the API
 * does. `parallel.max` matches the backend ceiling (and its DB check
 * constraint) exactly: a wizard that offered more would collect a value the API
 * then rejects.
 */
export const LIMITS = {
  parallel: { min: 1, max: 16 },
  queueDepth: { min: 1, max: 500 },
  timeout: { min: 5, max: 600 },
  /** Coalescing: window in seconds, and the key template's size in bytes. */
  coalesce: { window: { min: 1, max: 300 }, keyBytes: 256 },
  /** A routed trigger's join key template, in bytes. */
  joinKeyBytes: 256,
};

/** Icon per kind: what wakes this trigger up. One map, so every surface agrees. */
export const KIND_ICON: Record<ConsoleKind, IconName> = {
  webhook: 'webhook',
  chat: 'send',
  scheduled: 'clock',
  manual: 'play',
};

export interface SourceRow {
  id: string;
  name: string;
  provider: string;
  /** `chat` for a chat platform's source, else `webhook`. */
  category: SourceCategory;
  enabled: boolean;
  /** False for a source whose provider issues its secret and that is still waiting for it. */
  signingSecretSet: boolean;
  createdAt: string;
  /**
   * Requests that failed authentication. Never deliveries, since those are
   * unauthenticated requests; surfaced here so a wrong signing secret is
   * diagnosable rather than silent.
   */
  rejectionCount: number;
  lastRejectionAt: string | null;
  lastRejectionReason: string | null;
}

export interface TriggerRow {
  id: string;
  kind: TriggerKind;
  sourceId: string;
  name: string;
  /** "routed" triggers have no single agent: both agent fields are empty. */
  targetKind: TriggerTargetKind;
  agentTypeId: string;
  agentSlug: string;
  /** Routed triggers only. */
  routing: TriggerRoutingRow | null;
  /** Webhook triggers only. */
  eventType: string;
  matchCondition: Record<string, string[]>;
  /** Webhook triggers only. field → substrings, matched ignoring case. */
  matchContains: Record<string, string[]>;
  /** Webhook and chat command triggers only. Both lists empty means anyone may fire it. */
  senderRestriction: Senders;
  /** Webhook triggers only. Null when bursts are not coalesced. */
  coalesce: TriggerCoalesce | null;
  promptTemplate: string;
  /** Scheduled triggers only. */
  scheduleConfig?: { cronExpression: string; timezone: string; catchUp?: boolean };
  /** Manual triggers only: the inputs this trigger declares. */
  manualInputs?: InputField[];
  enabled: boolean;
  createdAt: string;

  /** Admission control. */
  maxParallel: number;
  overflow: TriggerOverflow;
  maxQueueDepth: number;

  /** Delivery safety. */
  timeoutSeconds: number;
  dedupeEnabled: boolean;
  autopauseEnabled: boolean;
  consecutiveFailures: number;
  /**
   * Set when the platform paused this trigger itself after sustained failure.
   * Shown differently from a trigger a person turned off: one needs
   * investigating, the other was deliberate.
   */
  autoPausedAt: string | null;
  /**
   * Deliveries admitted and queued for this trigger, not yet started. Zero
   * until the backend reports it, so a console deployed first shows nothing
   * rather than a wrong number.
   */
  pendingCount: number;
  /** Whether the caller may fire this trigger. */
  canFire: boolean;
}

export interface DeliveryRow {
  id: string;
  kind: TriggerKind;
  sourceId: string;
  eventType: string;
  status: string;
  triggerId: string | null;
  /** Denormalised on the delivery, so the feed can name a trigger it never loaded. */
  triggerName: string | null;
  runId: string | null;
  error: string | null;
  payloadExcerpt: string | null;
  /** Principal ID of the caller who fired a manual trigger. */
  firedBy: string | null;
  /** Cron tick time for scheduled deliveries. */
  scheduledAt: string | null;
  /** Why a non-dispatch arrival ended as it did (duplicate, missed). */
  reason: string | null;
  payloadBytes: number | null;
  payloadPurged: boolean;
  receivedAt: string;
  /** Full per-trigger dispatch fan-out for this delivery. */
  runs: DeliveryRunRow[];
  /** The coalescing key each matched coalescing trigger rendered for this delivery. */
  coalesceKeys: DeliveryCoalesceKeyRow[];
}

export interface DeliveryCoalesceKeyRow {
  triggerId: string;
  /** Null when the rendered key was too long to use. */
  key: string | null;
  /** The key exceeded the size limit, so this fire did not coalesce. */
  keyTooLong: boolean;
}

export interface DeliveryRunRow {
  triggerId: string;
  /** Present once the API denormalises it; the id is the fallback. */
  triggerName: string | null;
  runId: string | null;
  status: string;
  error: string | null;
  /** The delivery whose fire replaced this one; set when the status is `superseded`. */
  supersededBy?: string;
  /** The decision a routed trigger made for this delivery. Absent on every other trigger. */
  routing?: DeliveryRoutingRow;
  /**
   * When this outcome was recorded. Against the delivery's receivedAt this is
   * how long the platform took to start the run, which is the only dispatch
   * latency signal available without instrumenting the agent API.
   */
  createdAt: string | null;
}

/** What a routed trigger decided for one delivery. */
export interface DeliveryRoutingRow {
  outcome: TriggerRouteOutcome;
  keyMatched: boolean;
  joinKey: string | null;
  /** Null when the join key decided without a model call. */
  model: string | null;
  /** The agent that received the delivery. */
  agentTypeId: string | null;
  /** The chosen option's probability. */
  probability: number | null;
  threshold: number | null;
  /** Each candidate agent's probability, highest first. */
  agentScores: { agentTypeId: string; probability: number }[];
  /** below_threshold, decision_failed or run_closed. */
  fallbackReason: string | null;
  error: string | null;
}

/** Minimal agent identity used by the trigger create/edit form's agent picker. */
export interface AgentOption {
  id: string;
  slug: string;
  name: string;
  /**
   * Identity model of the agent. Delegated agents cannot be bound to triggers
   * (they only run on behalf of a person), so the picker disables them.
   */
  identityMode: 'autonomous' | 'delegated';
  /** Whether the caller may run this agent; binding and run-shaping edits need it. */
  canRun: boolean;
}

// ── Transformers ──────────────────────────────────────────────────────────────

/** The agents a trigger can start: its bound agent, or every candidate of a routed trigger. */
export function runTargets(t: Pick<TriggerRow, 'agentTypeId' | 'routing'>): string[] {
  return t.routing ? t.routing.candidates : [t.agentTypeId];
}

export function toSourceRow(s: WebhookSource): SourceRow {
  return {
    id: s.id,
    name: s.name,
    provider: s.provider,
    // A backend that predates categories has only webhook sources.
    category: s.category ?? 'webhook',
    enabled: s.enabled,
    signingSecretSet: s.signing_secret_set ?? true,
    createdAt: s.created_at,
    rejectionCount: s.rejection_count ?? 0,
    lastRejectionAt: s.last_rejection_at ?? null,
    lastRejectionReason: s.last_rejection_reason ?? null,
  };
}

export function toTriggerRow(t: Trigger): TriggerRow {
  return {
    id: t.id,
    kind: t.kind,
    sourceId: t.source_id ?? '',
    name: t.name,
    targetKind: t.target_kind ?? 'agent',
    agentTypeId: t.agent_type_id ?? '',
    agentSlug: t.agent_slug ?? '',
    routing: t.routing ? toRoutingRow(t.routing) : null,
    eventType: t.event_type ?? '',
    matchCondition: t.match_condition ?? {},
    matchContains: t.match_contains ?? {},
    senderRestriction: {
      ids: t.sender_restriction?.ids ?? [],
      roles: t.sender_restriction?.roles ?? [],
    },
    coalesce: t.coalesce ? { key: t.coalesce.key, windowSeconds: t.coalesce.window_seconds } : null,
    promptTemplate: t.prompt_template,
    scheduleConfig: t.schedule_config
      ? {
          cronExpression: t.schedule_config.cron_expression,
          timezone: t.schedule_config.timezone,
          catchUp: t.schedule_config.catch_up ?? false,
        }
      : undefined,
    manualInputs: t.manual_config?.inputs,
    enabled: t.enabled,
    createdAt: t.created_at,
    maxParallel: t.max_parallel ?? 4,
    overflow: t.overflow ?? 'wait',
    maxQueueDepth: t.max_queue_depth ?? 32,
    timeoutSeconds: t.timeout_seconds ?? 30,
    dedupeEnabled: t.dedupe_enabled ?? true,
    autopauseEnabled: t.autopause_enabled ?? true,
    consecutiveFailures: t.consecutive_failures ?? 0,
    autoPausedAt: t.auto_paused_at ?? null,
    // Zero until the backend reports it, so a console deployed ahead of the
    // API shows nothing rather than a wrong number.
    pendingCount: t.pending_count ?? 0,
    canFire: t.can_fire ?? false,
  };
}

export function toDeliveryRow(d: TriggerDelivery): DeliveryRow {
  // trigger_name falls back to null, so a console deployed ahead of the API
  // shows the id fallback rather than breaking; no lockstep deploy required.
  return {
    id: d.id,
    kind: d.kind,
    sourceId: d.source_id ?? '',
    eventType: d.event_type ?? '',
    status: d.status,
    triggerId: d.trigger_id ?? null,
    triggerName: d.trigger_name ?? null,
    runId: d.run_id ?? null,
    error: d.error ?? null,
    payloadExcerpt: d.payload_excerpt ?? null,
    firedBy: d.fired_by ?? null,
    scheduledAt: d.scheduled_at ?? null,
    reason: d.reason ?? null,
    payloadBytes: d.payload_bytes ?? null,
    payloadPurged: d.payload_purged ?? false,
    receivedAt: d.received_at,
    runs: (d.runs ?? []).map((r) => ({
      triggerId: r.trigger_id,
      triggerName: r.trigger_name ?? null,
      runId: r.run_id ?? null,
      status: r.status,
      error: r.error ?? null,
      ...(r.superseded_by !== undefined && { supersededBy: r.superseded_by }),
      ...(r.routing !== undefined && { routing: toDeliveryRoutingRow(r.routing) }),
      createdAt: r.created_at ?? null,
    })),
    coalesceKeys: (d.coalesce_keys ?? []).map((c) => ({
      triggerId: c.trigger_id,
      key: c.key ?? null,
      keyTooLong: c.key_too_long ?? false,
    })),
  };
}

export function toRoutingRow(r: TriggerRouting): TriggerRoutingRow {
  return {
    candidates: r.candidates ?? [],
    model: r.model,
    confidence: r.confidence,
    fallbackAgentTypeId: r.fallback_agent_type_id ?? null,
    joinRuns: r.join_runs ?? true,
    joinKey: r.join_key ?? '',
  };
}

export function toDeliveryRoutingRow(d: TriggerRoutingDecision): DeliveryRoutingRow {
  return {
    outcome: d.outcome,
    keyMatched: d.key_matched,
    joinKey: d.join_key ?? null,
    model: d.model ?? null,
    agentTypeId: d.agent_type_id ?? null,
    probability: d.probability ?? null,
    threshold: d.threshold ?? null,
    agentScores: Object.entries(d.scores?.agents ?? {})
      .map(([agentTypeId, probability]) => ({ agentTypeId, probability }))
      .sort((a, b) => b.probability - a.probability),
    fallbackReason: d.fallback_reason ?? null,
    error: d.error ?? null,
  };
}

// ── Request mapping ───────────────────────────────────────────────────────────

/**
 * The wire form of a routing object. Shared by create and update. A join key
 * is sent only while joining is on, since the backend reads it for nothing else.
 */
export function toRoutingRequest(r: TriggerRoutingRow): TriggerRouting {
  const joinKey = r.joinKey.trim();
  return {
    candidates: r.candidates,
    model: r.model,
    confidence: r.confidence,
    fallback_agent_type_id: r.fallbackAgentTypeId,
    join_runs: r.joinRuns,
    ...(r.joinRuns && joinKey !== '' && { join_key: joinKey }),
  };
}

/** A schedule as the console holds it, before it is put on the wire. */
export interface TriggerScheduleDraft {
  cronExpression: string;
  timezone: string;
  catchUp?: boolean;
}

/**
 * A correction to a stored trigger, in the console's own vocabulary. Every
 * field is optional: a caller sends only what it owns, so adjusting a limit
 * cannot disturb a prompt.
 */
export interface TriggerPatch {
  name?: string;
  promptTemplate?: string;
  /** Webhook only. */
  eventType?: string;
  matchCondition?: Record<string, string[]>;
  /** Webhook only. Replaces the contains filters; empty removes them. */
  matchContains?: Record<string, string[]>;
  /** Webhook only. Replaces the restriction; both lists empty removes it. */
  senderRestriction?: Senders;
  /** Webhook only. Replaces the setting; null removes it. */
  coalesce?: TriggerCoalesce | null;
  scheduleConfig?: TriggerScheduleDraft;
  enabled?: boolean;
  dedupeEnabled?: boolean;
  autopauseEnabled?: boolean;
  maxParallel?: number;
  overflow?: TriggerOverflow;
  maxQueueDepth?: number;
  timeoutSeconds?: number;
}

/** The wire form of a coalesce setting. Shared by create and update. */
export function toCoalesceRequest(coalesce: TriggerCoalesce): TriggerCoalesceWire {
  return { key: coalesce.key, window_seconds: coalesce.windowSeconds };
}

/** The wire form of a schedule. Shared by create and update. */
export function toScheduleConfigRequest(schedule: TriggerScheduleDraft) {
  return {
    cron_expression: schedule.cronExpression,
    timezone: schedule.timezone,
    catch_up: schedule.catchUp ?? false,
  };
}

/**
 * The single camelCase → snake_case mapper for a trigger update.
 *
 * Written once so a renamed API field is one edit, and so no component has to
 * know the wire casing. Absent keys stay absent: PATCH semantics mean an
 * omitted field is unchanged, and sending `undefined` would risk clearing it.
 */
export function toTriggerUpdateRequest(patch: TriggerPatch): TriggerUpdateRequest {
  return {
    ...(patch.name !== undefined && { name: patch.name }),
    ...(patch.promptTemplate !== undefined && { prompt_template: patch.promptTemplate }),
    ...(patch.eventType !== undefined && { event_type: patch.eventType }),
    ...(patch.matchCondition !== undefined && { match_condition: patch.matchCondition }),
    ...(patch.matchContains !== undefined && { match_contains: patch.matchContains }),
    ...(patch.senderRestriction !== undefined && {
      sender_restriction: patch.senderRestriction,
    }),
    // `{}` is how the API removes the setting; omitting it leaves it alone.
    ...(patch.coalesce !== undefined && {
      coalesce: patch.coalesce === null ? {} : toCoalesceRequest(patch.coalesce),
    }),
    ...(patch.scheduleConfig !== undefined && {
      schedule_config: toScheduleConfigRequest(patch.scheduleConfig),
    }),
    ...(patch.enabled !== undefined && { enabled: patch.enabled }),
    ...(patch.dedupeEnabled !== undefined && { dedupe_enabled: patch.dedupeEnabled }),
    ...(patch.autopauseEnabled !== undefined && { autopause_enabled: patch.autopauseEnabled }),
    ...(patch.maxParallel !== undefined && { max_parallel: patch.maxParallel }),
    ...(patch.overflow !== undefined && { overflow: patch.overflow }),
    ...(patch.maxQueueDepth !== undefined && { max_queue_depth: patch.maxQueueDepth }),
    ...(patch.timeoutSeconds !== undefined && { timeout_seconds: patch.timeoutSeconds }),
  };
}

// ── Display helpers shared by the trigger and delivery surfaces ──────────────

/**
 * Characters of an id worth showing.
 *
 * Enough to correlate a row with a log line, short enough that an id never
 * wraps a table row onto a second line. Full ids stay available in the panel.
 */
export const SHORT_ID_LENGTH = 8;

export function shortId(id: string): string {
  return id.length > SHORT_ID_LENGTH ? id.slice(0, SHORT_ID_LENGTH) : id;
}

/**
 * Relative time, because "2m ago" answers the question and a timestamp does
 * not. Shared, so the trigger list and the delivery list cannot phrase the same
 * gap two ways.
 */
export function relativeTime(iso: string | null): string {
  if (iso === null) return 'Never';
  const ms = Date.now() - Date.parse(iso);
  if (!Number.isFinite(ms)) return 'Never';
  const mins = Math.floor(ms / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

/**
 * The absolute time for hover titles, the delivery panel, and the agent page:
 * one formatter, so those surfaces spell the same instant the same way. The
 * drawer's delivery history deliberately uses a different-grain formatter.
 */
export function formatTimestampUtc(iso: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${d.toISOString().slice(0, 19).replace('T', ' ')} UTC`;
}

/** Maps a delivery status to a StatusBadge tone. */
export function deliveryStatusTone(status: string): 'healthy' | 'degraded' | 'error' | 'unknown' {
  switch (status) {
    case 'dispatched':
    case 'joined':
      return 'healthy';
    case 'verified':
    case 'skipped':
    // A routed trigger that was not sure enough and had no fallback worked as configured.
    case 'unrouted':
    // A repeat arrival and a tick that fired while the platform was away are
    // notable but not faults; they share the cautionary tone with skipped.
    case 'duplicate':
    case 'missed':
      return 'degraded';
    case 'failed':
    case 'denied':
    case 'dropped':
      return 'error';
    default:
      return 'unknown';
  }
}

/**
 * StatusDot has no "unknown" tone. An outcome the console does not recognise is
 * shown as idle rather than as healthy or failed: inventing a verdict for a
 * status we have not taught the UI about is worse than showing none.
 */
export function deliveryDotStatus(status: string): 'healthy' | 'degraded' | 'error' | 'idle' {
  const tone = deliveryStatusTone(status);
  return tone === 'unknown' ? 'idle' : tone;
}

/**
 * Which trigger this delivery was for, in the best terms available.
 *
 * A delivery is an arrival, not a dispatch: one can fan out to several triggers
 * or to none at all. The denormalised name is preferred, then the fan-out's own
 * names, then a shortened id, and finally nothing at all for an arrival that
 * matched no trigger, which is a real and interesting outcome rather than
 * missing data. `kind` lets a caller render an id differently from a name; the
 * id is already shortened, because a uuid wrapped over three lines is what turns
 * a list into a wall.
 */
export type DeliveryTriggerIdentity =
  { kind: 'name'; label: string } | { kind: 'id'; label: string } | { kind: 'none' };

export function deliveryTriggerIdentity(d: DeliveryRow): DeliveryTriggerIdentity {
  if (d.triggerName) return { kind: 'name', label: d.triggerName };

  const first = d.runs.find((r) => Boolean(r.triggerName) || Boolean(r.triggerId));
  if (first !== undefined) {
    const extra = d.runs.length - 1;
    const suffix = extra > 0 ? ` +${extra}` : '';
    return first.triggerName
      ? { kind: 'name', label: `${first.triggerName}${suffix}` }
      : { kind: 'id', label: `${shortId(first.triggerId)}${suffix}` };
  }

  return d.triggerId !== null ? { kind: 'id', label: shortId(d.triggerId) } : { kind: 'none' };
}

/** The identity as a single string, for surfaces that cannot style the two apart. */
export function deliveryTriggerLabel(d: DeliveryRow): string {
  const identity = deliveryTriggerIdentity(d);
  return identity.kind === 'none' ? '—' : identity.label;
}

/**
 * How long the platform took to turn an arrival into a recorded outcome.
 *
 * Null when the outcome carries no timestamp: an event that was filtered or
 * denied never reached a run, so there is no duration to report and a "0ms"
 * would read as suspiciously fast rather than as not-applicable.
 */
export function dispatchDuration(receivedAt: string, createdAt: string | null): string {
  if (createdAt === null) return '—';
  const ms = Date.parse(createdAt) - Date.parse(receivedAt);
  if (!Number.isFinite(ms) || ms < 0) return '—';
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
}

/** The dispatch duration for a delivery, from a trigger's row in its fan-out. */
export function deliveryDuration(d: DeliveryRow, triggerId?: string): string {
  const outcome =
    triggerId === undefined
      ? (d.runs[0] ?? null)
      : (d.runs.find((r) => r.triggerId === triggerId) ?? null);
  return dispatchDuration(d.receivedAt, outcome?.createdAt ?? null);
}

/**
 * Outcomes a replay could actually recover.
 *
 * The backend refuses what is genuinely unreplayable, but a delivery that
 * already dispatched is replayable and should still not be offered: one stray
 * click would start a second, duplicate run of work that already succeeded.
 *
 * `superseded` belongs here for the same reason as `dropped`: a delivery the
 * queue discarded in favour of a newer one never ran, so replaying it is the
 * only way to recover the work it was carrying.
 */
const REDELIVERABLE_STATUSES = new Set(['failed', 'denied', 'dropped', 'superseded']);

export function isRedeliverable(status: string): boolean {
  return REDELIVERABLE_STATUSES.has(status);
}

/**
 * What to show for the key a trigger rendered on a delivery.
 *
 * A key too long to use is stated plainly, since the fire ran on its own and
 * the reader would otherwise wonder why nothing was coalesced.
 */
export function coalesceKeyText(entry: DeliveryCoalesceKeyRow): string {
  if (entry.keyTooLong) return 'Key too long, not coalesced';
  return entry.key ?? '';
}

/**
 * The coalescing keys to show for a delivery. Scoped to one trigger when the
 * surface is, so a trigger's drawer never shows another trigger's key.
 */
export function coalesceKeysFor(d: DeliveryRow, triggerId?: string): DeliveryCoalesceKeyRow[] {
  return triggerId === undefined
    ? d.coalesceKeys
    : d.coalesceKeys.filter((c) => c.triggerId === triggerId);
}

/**
 * The runs of a delivery that a newer delivery replaced, with the replacing
 * delivery's id. Scoped to one trigger when the surface is.
 */
export function supersededRuns(d: DeliveryRow, triggerId?: string): DeliveryRunRow[] {
  return d.runs.filter(
    (r) => r.supersededBy !== undefined && (triggerId === undefined || r.triggerId === triggerId),
  );
}

/** Max characters of a payload excerpt rendered in the Deliveries "Detail" column. */
export const DELIVERY_EXCERPT_MAX = 120;

/**
 * Decision for the Deliveries "Detail" column, in priority order:
 * fan-out run count > error > single run id > payload excerpt > none.
 * Pure so the priority chain and excerpt truncation are unit-testable. The grid
 * reads it through `deliverySummary`, which clips it to one line and styles from
 * `kind`; the panel shows the untrimmed form.
 *
 * Note: `text` for `excerpt` is derived from `payload_excerpt`, which is
 * untrusted webhook data — it is rendered through Hades `<Text>` (React-escaped),
 * never as HTML.
 */
export type DeliveryDetail =
  | { kind: 'runs'; text: string }
  | { kind: 'error'; text: string }
  | { kind: 'runId'; text: string }
  | { kind: 'excerpt'; text: string }
  | { kind: 'empty' };

/**
 * The human end of a failure chain.
 *
 * Backend errors arrive as a wrapped chain, often ending in a JSON error body:
 * "start run: unexpected status from kora-agents: status 500: {\"code\":...,
 * \"message\":\"failed to start run\"}". The message inside that body is what a
 * person acts on; the full chain stays available as detail.
 */
export function failureSummary(error: string): string {
  const tail = error.match(/\{[^{}]*\}\s*$/);
  if (tail) {
    try {
      const body = JSON.parse(tail[0]) as { message?: unknown };
      if (typeof body.message === 'string' && body.message.trim() !== '') {
        return body.message;
      }
    } catch {
      // Not JSON after all; fall through to the chain.
    }
  }
  const parts = error.split(': ').map((p) => p.trim());
  return parts[parts.length - 1] || error;
}

export function deliveryDetail(row: DeliveryRow): DeliveryDetail {
  if (row.runs.length > 1) return { kind: 'runs', text: `${row.runs.length} runs` };
  if (row.error) return { kind: 'error', text: row.error };
  if (row.runId) return { kind: 'runId', text: `run ${row.runId.slice(0, 8)}` };
  if (row.payloadExcerpt) {
    const e = row.payloadExcerpt;
    return {
      kind: 'excerpt',
      text: e.length > DELIVERY_EXCERPT_MAX ? `${e.slice(0, DELIVERY_EXCERPT_MAX)}…` : e,
    };
  }
  return { kind: 'empty' };
}

/** Max characters of a one-line delivery summary, so every row stays one line. */
export const DELIVERY_SUMMARY_MAX = 72;

function clip(text: string): string {
  const flat = text.replace(/\s+/g, ' ').trim();
  return flat.length > DELIVERY_SUMMARY_MAX ? `${flat.slice(0, DELIVERY_SUMMARY_MAX)}…` : flat;
}

/**
 * One line about what a delivery did, for a list row.
 *
 * The same priority chain as `deliveryDetail`, reduced to something that fits a
 * cell: an error becomes its human end rather than the whole wrapped chain, and
 * everything is flattened to a single line. The full chain stays in the panel,
 * which is where a person goes once the summary tells them to look.
 */
export type DeliverySummary =
  { kind: 'runs' | 'error' | 'runId' | 'excerpt'; text: string } | { kind: 'none' };

export function deliverySummary(row: DeliveryRow): DeliverySummary {
  const detail = deliveryDetail(row);
  switch (detail.kind) {
    case 'empty':
      return { kind: 'none' };
    case 'error':
      return { kind: 'error', text: clip(failureSummary(detail.text)) };
    default:
      return { kind: detail.kind, text: clip(detail.text) };
  }
}

/**
 * Client-side Deliveries status-filter predicate. A row passes when its status
 * is in the selected set (OR semantics across multiple selections). An empty
 * selection matches nothing, so a caller that means "no filter" skips the
 * predicate rather than passing an empty set.
 */
export function matchesStatusFilter(row: DeliveryRow, selected: ReadonlySet<string>): boolean {
  return selected.has(row.status);
}
