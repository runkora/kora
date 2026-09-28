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

import type { IconName } from '@orbit/terra';
import type {
  InputField,
  SenderRestriction,
  Trigger,
  TriggerDelivery,
  TriggerKind,
  TriggerOverflow,
  TriggerUpdateRequest,
  WebhookSource,
} from '@orbit/shared/api';

export type { InputField, TriggerKind, TriggerOverflow };

/** Who may fire a webhook trigger; both lists empty means anyone. */
export type Senders = Required<SenderRestriction>;

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
};

/** Icon per kind: what wakes this trigger up. One map, so every surface agrees. */
export const KIND_ICON: Record<TriggerKind, IconName> = {
  webhook: 'webhook',
  scheduled: 'clock',
  manual: 'play',
};

export interface SourceRow {
  id: string;
  name: string;
  provider: string;
  enabled: boolean;
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
  agentTypeId: string;
  agentSlug: string;
  /** Webhook triggers only. */
  eventType: string;
  matchCondition: Record<string, string[]>;
  /** Webhook triggers only. field → substrings, matched ignoring case. */
  matchContains: Record<string, string[]>;
  /** Webhook triggers only. Both lists empty means anyone may fire it. */
  senderRestriction: Senders;
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
}

export interface DeliveryRunRow {
  triggerId: string;
  /** Present once the API denormalises it; the id is the fallback. */
  triggerName: string | null;
  runId: string | null;
  status: string;
  error: string | null;
  /**
   * When this outcome was recorded. Against the delivery's receivedAt this is
   * how long the platform took to start the run, which is the only dispatch
   * latency signal available without instrumenting the agent API.
   */
  createdAt: string | null;
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
}

// ── Transformers ──────────────────────────────────────────────────────────────

export function toSourceRow(s: WebhookSource): SourceRow {
  return {
    id: s.id,
    name: s.name,
    provider: s.provider,
    enabled: s.enabled,
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
    agentTypeId: t.agent_type_id,
    agentSlug: t.agent_slug,
    eventType: t.event_type ?? '',
    matchCondition: t.match_condition ?? {},
    matchContains: t.match_contains ?? {},
    senderRestriction: {
      logins: t.sender_restriction?.logins ?? [],
      associations: t.sender_restriction?.associations ?? [],
    },
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
      createdAt: r.created_at ?? null,
    })),
  };
}

// ── Request mapping ───────────────────────────────────────────────────────────

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
  scheduleConfig?: TriggerScheduleDraft;
  enabled?: boolean;
  dedupeEnabled?: boolean;
  autopauseEnabled?: boolean;
  maxParallel?: number;
  overflow?: TriggerOverflow;
  maxQueueDepth?: number;
  timeoutSeconds?: number;
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
      return 'healthy';
    case 'verified':
    case 'skipped':
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
 * untrusted webhook data — it is rendered through Terra `<Text>` (React-escaped),
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
 * "start run: unexpected status from orbit-agents: status 500: {\"code\":...,
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
