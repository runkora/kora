/**
 * General-purpose formatting utilities for the Orbit frontend.
 *
 * These helpers are UI-framework-agnostic and carry no dependency on any
 * feature, route segment, or component.
 *
 * Import via the utils barrel so internal restructuring never requires
 * updating call sites:
 *
 *   import { fmtDate, fmtCost } from '@orbit/utils';
 */

// ── Numbers ───────────────────────────────────────────────────────────────────

/**
 * Full locale-formatted integer, e.g. 1,234,567.
 * Use when the exact count matters (token totals, log row counts).
 */
export function fmtNum(n: number): string {
  return n.toLocaleString();
}

/**
 * Compact K/M abbreviation, e.g. 1.2K, 3.45M.
 * Use in space-constrained UI (stat cards, table cells) where an approximate
 * value is more readable than the full number.
 */
export function fmtNumCompact(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

// ── Cost ──────────────────────────────────────────────────────────────────────

/**
 * Formats a USD cost value for display.
 * - null/undefined → em dash (no data)
 * - 0              → "$0.00"
 * - < $0.0001      → "<$0.0001" (avoids displaying $0.0000 for tiny costs)
 * - otherwise      → 4 decimal places, e.g. "$0.0023"
 */
export function fmtCost(usd: number | null): string {
  if (usd === null || usd === undefined) return '—';
  if (usd === 0) return '$0.00';
  if (usd < 0.0001) return '<$0.0001';
  return `$${usd.toFixed(4)}`;
}

// ── Dates & times ─────────────────────────────────────────────────────────────

/**
 * Formats a non-nullable ISO timestamp as a short date, e.g. "Jan 1, 2025".
 * Intended for table cells where a date is always present (e.g. log rows).
 */
export function fmtDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  });
}

/**
 * Formats a non-nullable ISO timestamp as a time string, e.g. "12:34:56 PM".
 * Paired with fmtDate to show stacked date + time in log table cells.
 */
export function fmtTime(iso: string): string {
  return new Date(iso).toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

/**
 * Formats a nullable expiry timestamp for display in API key cards.
 * Returns "Never expires" when the value is absent, rather than a blank or
 * placeholder, so the UI always has a readable string to render.
 */
export function fmtExpiry(iso: string | null | undefined): string {
  if (!iso) return 'Never expires';
  return new Date(iso).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
}

// ── Expiry check ──────────────────────────────────────────────────────────────

/**
 * Returns true if the given expiry timestamp is in the past.
 * Returns false when expiresAt is null/undefined (no expiry = never expires).
 */
export function isExpired(expiresAt: string | null | undefined): boolean {
  if (!expiresAt) return false;
  return new Date(expiresAt) < new Date();
}
