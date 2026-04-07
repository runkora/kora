import type { Period } from '../table-controls';

// ── Date helpers ──────────────────────────────────────────────────────────────

function subtractDays(date: Date, days: number): Date {
  const d = new Date(date);
  d.setDate(d.getDate() - days);
  return d;
}

function toDateStr(d: Date): string {
  return d.toISOString().slice(0, 10);
}

const PERIOD_DAYS: Record<Period, number> = {
  '7d': 7,
  '14d': 14,
  '30d': 30,
  '90d': 90,
  '180d': 180,
  '365d': 365,
};

// ── Filter ────────────────────────────────────────────────────────────────────

/**
 * Filters an array of rows that have a `date: string` field (ISO format)
 * to only include entries within the given Period window.
 *
 * `today` defaults to `new Date()` so production code always reflects the
 * real current date. Tests can pass a fixed date for deterministic results.
 */
export function filterByPeriod<T extends { date: string }>(
  rows: T[],
  period: Period,
  today = new Date(),
): T[] {
  const cutoff = toDateStr(subtractDays(today, PERIOD_DAYS[period]));
  return rows.filter((r) => r.date >= cutoff);
}

// ── Aggregation ───────────────────────────────────────────────────────────────

/** Returns the Monday of the ISO week containing `date` */
function getWeekStart(date: Date): string {
  const d = new Date(date);
  const day = d.getDay(); // 0 = Sun
  const diff = day === 0 ? -6 : 1 - day;
  d.setDate(d.getDate() + diff);
  return toDateStr(d);
}

/** Returns a "YYYY-MM" string for month-level aggregation */
function getMonthKey(date: Date): string {
  return toDateStr(date).slice(0, 7);
}

/**
 * Aggregates a daily dataset by ISO week.
 * `merge` receives all rows in the week and returns the numeric fields to sum.
 */
export function aggregateByWeek<T extends { date: string }, R>(
  rows: T[],
  merge: (rows: T[]) => R,
): Array<{ period: string } & R> {
  const buckets = new Map<string, T[]>();
  for (const row of rows) {
    const key = getWeekStart(new Date(row.date));
    if (!buckets.has(key)) buckets.set(key, []);
    buckets.get(key)!.push(row);
  }
  return Array.from(buckets.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, rs]) => ({ period: `Week of ${key}`, ...merge(rs) }));
}

/**
 * Aggregates a daily dataset by calendar month.
 * `merge` receives all rows in the month and returns the numeric fields to sum.
 */
export function aggregateByMonth<T extends { date: string }, R>(
  rows: T[],
  merge: (rows: T[]) => R,
): Array<{ period: string } & R> {
  const buckets = new Map<string, T[]>();
  for (const row of rows) {
    const key = getMonthKey(new Date(row.date));
    if (!buckets.has(key)) buckets.set(key, []);
    buckets.get(key)!.push(row);
  }
  return Array.from(buckets.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, rs]) => ({ period: key, ...merge(rs) }));
}
