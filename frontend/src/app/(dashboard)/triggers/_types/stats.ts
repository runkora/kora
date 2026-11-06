// Trigger stats as the console shows them. The API counts them in Postgres
// over the last 24 hours; this module only reshapes them for display.

import type { TenantTriggerStats, TriggerStats } from '@runkora/shared/api';
import type { TriggerRow } from './index';

/** One two-hour bucket of a trigger's outcomes, oldest first. */
export interface StatsBucketRow {
  start: string;
  started: number;
  failed: number;
  /** Outcomes that were neither: filtered, superseded, paused, unrouted, missed. */
  other: number;
}

/** A trigger's activity, counted by its own outcome for each delivery that reached it. */
export interface TriggerStatsRow {
  /** Newest delivery the trigger attempted a run for, or null if it never has. */
  lastFiredAt: string | null;
  deliveries24h: number;
  started24h: number;
  failed24h: number;
  /** Last 24h by the `status` filter value; a missing value is zero. */
  status24h: Record<string, number>;
  buckets: StatsBucketRow[];
}

/** The tenant's trigger activity over the last 24 hours. */
export interface TenantStatsRow {
  deliveries24h: number;
  started24h: number;
  failed24h: number;
  dispatchP50Ms: number | null;
  dispatchP95Ms: number | null;
}

export const EMPTY_TRIGGER_STATS: TriggerStatsRow = {
  lastFiredAt: null,
  deliveries24h: 0,
  started24h: 0,
  failed24h: 0,
  status24h: {},
  buckets: [],
};

export function toTriggerStatsRow(s: TriggerStats | undefined): TriggerStatsRow {
  if (s === undefined) return EMPTY_TRIGGER_STATS;
  return {
    lastFiredAt: s.last_fired_at,
    deliveries24h: s.deliveries_24h,
    started24h: s.started_24h,
    failed24h: s.failed_24h,
    status24h: s.status_24h,
    buckets: s.buckets,
  };
}

export function toTenantStatsRow(s: TenantTriggerStats): TenantStatsRow {
  return {
    deliveries24h: s.deliveries_24h,
    started24h: s.started_24h,
    failed24h: s.failed_24h,
    dispatchP50Ms: s.dispatch_p50_ms === null ? null : Math.round(s.dispatch_p50_ms),
    dispatchP95Ms: s.dispatch_p95_ms === null ? null : Math.round(s.dispatch_p95_ms),
  };
}

/**
 * Share of started outcomes among started and failed, as a whole percentage.
 * Null when neither happened: 0% would read as failure where there was silence.
 */
export function successRate(s: { started24h: number; failed24h: number }): number | null {
  const decided = s.started24h + s.failed24h;
  return decided === 0 ? null : Math.round((s.started24h / decided) * 100);
}

/**
 * A trigger wants a person: auto-paused, or enabled and part-way to the pause.
 * A trigger someone turned off does not count.
 */
export function needsAttention(t: TriggerRow): boolean {
  return t.autoPausedAt !== null || (t.enabled && t.consecutiveFailures > 0);
}
