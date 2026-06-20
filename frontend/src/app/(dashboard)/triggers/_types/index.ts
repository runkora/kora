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
