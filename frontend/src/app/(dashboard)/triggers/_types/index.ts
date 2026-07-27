// ── Display-layer types for the Triggers feature ─────────────────────────────
//
// *Row types are the shapes consumed by the UI. The raw API shapes
// (WebhookSource, Trigger, TriggerDelivery) only appear at the service boundary.

import type { Trigger, TriggerDelivery, TriggerKind, WebhookSource } from '@orbit/shared/api';

export type { TriggerKind };

export interface SourceRow {
  id: string;
  name: string;
  provider: string;
  enabled: boolean;
  createdAt: string;
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
  scheduleConfig?: { cronExpression: string; timezone: string };
  enabled: boolean;
  createdAt: string;
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
  receivedAt: string;
  /** Full per-trigger dispatch fan-out for this delivery. */
  runs: DeliveryRunRow[];
}

export interface DeliveryRunRow {
  triggerId: string;
  runId: string | null;
  status: string;
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
}

// ── Transformers ──────────────────────────────────────────────────────────────

export function toSourceRow(s: WebhookSource): SourceRow {
  return {
    id: s.id,
    name: s.name,
    provider: s.provider,
    enabled: s.enabled,
    createdAt: s.created_at,
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
      ? { cronExpression: t.schedule_config.cron_expression, timezone: t.schedule_config.timezone }
      : undefined,
    enabled: t.enabled,
    createdAt: t.created_at,
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
    receivedAt: d.received_at,
    runs: (d.runs ?? []).map((r) => ({
      triggerId: r.trigger_id,
      runId: r.run_id ?? null,
      status: r.status,
      error: r.error ?? null,
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
