// ── Display-layer types for the Triggers feature ─────────────────────────────
//
// *Row types are the shapes consumed by the UI. The raw API shapes
// (WebhookSource, Trigger, TriggerDelivery) only appear at the service boundary.

import type {
  InputField,
  Trigger,
  TriggerDelivery,
  TriggerKind,
  TriggerOverflow,
  WebhookSource,
} from '@orbit/shared/api';

export type { InputField, TriggerKind, TriggerOverflow };

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
}

export interface DeliveryRow {
  id: string;
  kind: TriggerKind;
  sourceId: string;
  eventType: string;
  status: string;
  triggerId: string | null;
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
  };
}

export function toDeliveryRow(d: TriggerDelivery): DeliveryRow {
  return {
    id: d.id,
    kind: d.kind,
    sourceId: d.source_id ?? '',
    eventType: d.event_type ?? '',
    status: d.status,
    triggerId: d.trigger_id ?? null,
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
      runId: r.run_id ?? null,
      status: r.status,
      error: r.error ?? null,
      createdAt: r.created_at ?? null,
    })),
  };
}

/** Maps a delivery status to a StatusBadge tone. */
export function deliveryStatusTone(status: string): 'healthy' | 'degraded' | 'error' | 'unknown' {
  switch (status) {
    case 'dispatched':
      return 'healthy';
    case 'verified':
    case 'skipped':
      return 'degraded';
    case 'failed':
      return 'error';
    default:
      return 'unknown';
  }
}

/** Max characters of a payload excerpt rendered in the Deliveries "Detail" column. */
export const DELIVERY_EXCERPT_MAX = 120;

/**
 * Decision for the Deliveries "Detail" column, in priority order:
 * fan-out run count > error > single run id > payload excerpt > none.
 * Pure so the priority chain and excerpt truncation are unit-testable; the
 * cell renders styling from `kind`.
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

/**
 * Client-side Deliveries status-filter predicate. A row passes when its status
 * is in the selected set (OR semantics across multiple selections). The
 * DataTable skips filtering entirely when nothing is selected, so an empty
 * selection shows all rows.
 */
export function matchesStatusFilter(row: DeliveryRow, selected: ReadonlySet<string>): boolean {
  return selected.has(row.status);
}
