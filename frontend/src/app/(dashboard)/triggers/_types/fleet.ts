// ── Fleet health derivation ──────────────────────────────────────────────────
//
// Every number the console shows about the trigger fleet is computed here, from
// the same rows the tables render. Nothing is a separately maintained counter:
// a KPI that can disagree with the list beneath it is worse than no KPI, because
// it is believed.
//
// Pure functions with no React, so the arithmetic is testable on its own.

import type { DeliveryRow, TriggerRow } from './index';

/** Delivery outcomes that mean a run started. */
const DISPATCHED = 'dispatched';

/**
 * Outcomes that represent something going wrong, as opposed to something being
 * declined on purpose.
 *
 * `filtered` and `superseded` are deliberately absent: a trigger declining an
 * event via its own match condition, or a newer fire replacing an older one,
 * are the configuration working. Counting them as failures would make a
 * correctly-configured fleet look unhealthy.
 */
const FAILURE_OUTCOMES = new Set(['failed', 'denied', 'dropped']);

/** Aggregate health of the trigger fleet, all derived from the rows shown. */
export interface FleetStats {
  activeTriggers: number;
  totalTriggers: number;
  /** Deliveries received in the last 24 hours. */
  deliveries24h: number;
  /**
   * Share of the last 24 hours' per-trigger outcomes that started a run.
   * Null when nothing arrived: 0% would imply failure where there was silence.
   */
  successRate: number | null;
  /** Triggers that are auto-paused or failing, and want a human. */
  needsAttention: number;
}

const DAY_MS = 24 * 60 * 60 * 1000;

function isWithin24h(iso: string, now: number): boolean {
  const t = Date.parse(iso);
  return Number.isFinite(t) && now - t <= DAY_MS;
}

/**
 * needsAttention counts triggers the platform cannot fix by itself.
 *
 * An auto-paused trigger always counts: the platform disabled it and only a
 * person can decide the cause is fixed. A trigger part-way to that threshold
 * counts too, so the fleet view warns before the pause rather than after.
 * A trigger a person deliberately turned off does not count.
 */
export function needsAttention(t: TriggerRow): boolean {
  return t.autoPausedAt !== null || (t.enabled && t.consecutiveFailures > 0);
}

/** Computes the fleet KPI row from the triggers and deliveries on screen. */
export function fleetStats(
  triggers: TriggerRow[],
  deliveries: DeliveryRow[],
  now: number = Date.now(),
): FleetStats {
  const recent = deliveries.filter((d) => isWithin24h(d.receivedAt, now));

  // Counted per trigger outcome rather than per delivery: one arrival can start
  // three runs and be denied by a fourth trigger, and a single verdict for the
  // whole arrival would hide that.
  let started = 0;
  let decided = 0;
  for (const d of recent) {
    for (const r of d.runs) {
      if (r.status === DISPATCHED) {
        started++;
        decided++;
      } else if (FAILURE_OUTCOMES.has(r.status)) {
        decided++;
      }
    }
  }

  return {
    activeTriggers: triggers.filter((t) => t.enabled).length,
    totalTriggers: triggers.length,
    deliveries24h: recent.length,
    successRate: decided === 0 ? null : Math.round((started / decided) * 100),
    needsAttention: triggers.filter(needsAttention).length,
  };
}

/** One bucket of a trigger's recent delivery history. */
export interface SparkBucket {
  started: number;
  failed: number;
  /** Outcomes that were neither: filtered, superseded, paused. */
  other: number;
}

/** How many buckets a sparkline shows: one per two hours over a day. */
export const SPARK_BUCKETS = 12;

/**
 * sparkline summarises a trigger's last 24 hours as fixed-width buckets.
 *
 * Buckets are always returned in full, including empty ones, so a quiet trigger
 * reads as quiet rather than as a shorter chart than its neighbour.
 */
export function sparkline(
  triggerId: string,
  deliveries: DeliveryRow[],
  now: number = Date.now(),
): SparkBucket[] {
  const buckets: SparkBucket[] = Array.from({ length: SPARK_BUCKETS }, () => ({
    started: 0,
    failed: 0,
    other: 0,
  }));
  const bucketMs = DAY_MS / SPARK_BUCKETS;

  for (const d of deliveries) {
    const t = Date.parse(d.receivedAt);
    if (!Number.isFinite(t) || now - t > DAY_MS || t > now) continue;

    const idx = Math.min(SPARK_BUCKETS - 1, Math.floor((t - (now - DAY_MS)) / bucketMs));
    for (const r of d.runs) {
      if (r.triggerId !== triggerId) continue;
      if (r.status === DISPATCHED) buckets[idx].started++;
      else if (FAILURE_OUTCOMES.has(r.status)) buckets[idx].failed++;
      else buckets[idx].other++;
    }
  }
  return buckets;
}

/** Recent failure count for a trigger, used for the row's failure badge. */
export function recentFailures(
  triggerId: string,
  deliveries: DeliveryRow[],
  now: number = Date.now(),
): number {
  let n = 0;
  for (const d of deliveries) {
    if (!isWithin24h(d.receivedAt, now)) continue;
    for (const r of d.runs) {
      if (r.triggerId === triggerId && FAILURE_OUTCOMES.has(r.status)) n++;
    }
  }
  return n;
}

/** Timestamp of a trigger's most recent delivery, or null if it has never fired. */
export function lastFired(triggerId: string, deliveries: DeliveryRow[]): string | null {
  let latest: string | null = null;
  for (const d of deliveries) {
    if (d.triggerId !== triggerId && !d.runs.some((r) => r.triggerId === triggerId)) continue;
    if (latest === null || Date.parse(d.receivedAt) > Date.parse(latest)) latest = d.receivedAt;
  }
  return latest;
}
