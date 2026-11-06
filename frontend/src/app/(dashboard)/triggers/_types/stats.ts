// Trigger stats as the console shows them. The API counts them in Postgres
// over the selected window; this module only reshapes them for display.

import type { TenantTriggerStats, TriggerStats, TriggerStatsWindow } from '@runkora/shared/api';
import type { TriggerRow } from './index';

export type StatsWindow = TriggerStatsWindow;

/** The window a page opens on. The server render fetches it, so first paint has data. */
export const DEFAULT_STATS_WINDOW: StatsWindow = '24h';

const WINDOW_MS: Record<StatsWindow, number> = { '24h': 24 * 3600_000, '7d': 7 * 24 * 3600_000 };

/** The options the range selector offers, in the Agents page's label style. */
export const STATS_WINDOW_OPTIONS = (Object.keys(WINDOW_MS) as StatsWindow[]).map((value) => ({
  value,
  label: value,
}));

export function isStatsWindow(value: string): value is StatsWindow {
  return Object.hasOwn(WINDOW_MS, value);
}

/** The start of the window ending now, as the `received_after` a delivery list is bounded by. */
export function windowStart(w: StatsWindow, now = Date.now()): string {
  return new Date(now - WINDOW_MS[w]).toISOString();
}

/** One bucket of a trigger's outcomes, oldest first. */
export interface StatsBucketRow {
  start: string;
  started: number;
  failed: number;
  /** Outcomes that were neither: filtered, skipped, superseded, duplicate, missed, verified. */
  other: number;
}

/** A trigger's activity, counted by its own outcome for each delivery that reached it. */
export interface TriggerStatsRow {
  /** Newest delivery the trigger attempted a run for, or null if it never has. */
  lastFiredAt: string | null;
  deliveries: number;
  started: number;
  failed: number;
  /** The window by trigger status, the `status` filter value; a missing value is zero. */
  statusCounts: Record<string, number>;
  buckets: StatsBucketRow[];
}

/** The tenant's trigger activity over the window. */
export interface TenantStatsRow {
  deliveries: number;
  started: number;
  failed: number;
  dispatchP50Ms: number | null;
  dispatchP95Ms: number | null;
}

export const EMPTY_TRIGGER_STATS: TriggerStatsRow = {
  lastFiredAt: null,
  deliveries: 0,
  started: 0,
  failed: 0,
  statusCounts: {},
  buckets: [],
};

export function toTriggerStatsRow(s: TriggerStats | undefined): TriggerStatsRow {
  if (s === undefined) return EMPTY_TRIGGER_STATS;
  return {
    lastFiredAt: s.last_fired_at,
    deliveries: s.deliveries,
    started: s.started,
    failed: s.failed,
    statusCounts: s.status_counts,
    buckets: s.buckets,
  };
}

export function toTenantStatsRow(s: TenantTriggerStats): TenantStatsRow {
  return {
    deliveries: s.deliveries,
    started: s.started,
    failed: s.failed,
    dispatchP50Ms: s.dispatch_p50_ms === null ? null : Math.round(s.dispatch_p50_ms),
    dispatchP95Ms: s.dispatch_p95_ms === null ? null : Math.round(s.dispatch_p95_ms),
  };
}

/**
 * Share of started outcomes among started and failed, as a whole percentage.
 * Null when neither happened: 0% would read as failure where there was silence.
 */
export function successRate(s: { started: number; failed: number }): number | null {
  const decided = s.started + s.failed;
  return decided === 0 ? null : Math.round((s.started / decided) * 100);
}

/**
 * Activity per bucket for a sparkline: started and failed only, so it agrees
 * with "Last fired". A trigger that only declined draws nothing.
 */
export function firedPerBucket(s: TriggerStatsRow): number[] {
  return s.buckets.map((b) => b.started + b.failed);
}

/**
 * A trigger wants a person: auto-paused, or enabled and part-way to the pause.
 * A trigger someone turned off does not count.
 */
export function needsAttention(t: TriggerRow): boolean {
  return t.autoPausedAt !== null || (t.enabled && t.consecutiveFailures > 0);
}
