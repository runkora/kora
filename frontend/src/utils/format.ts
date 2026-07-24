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
 * Full integer with thousands separators, e.g. 1,234,567.
 * Use when the exact count matters (token totals, log row counts).
 *
 * Locale is pinned to 'en-US' (rather than the runtime/browser locale) so
 * server-rendered and client-rendered output always match, avoiding React
 * hydration mismatches.
 */
export function fmtNum(n: number): string {
  return n.toLocaleString('en-US');
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

/**
 * Formats a USD budget value at 2 decimal places, e.g. "$50.00", "$1,234.56".
 * Use for user-configured budget fields (role or grant budget assignments) where
 * sub-cent precision is not meaningful and readability matters.
 */
export function fmtBudget(usd: number | null): string {
  if (usd === null || usd === undefined) return '—';
  return `$${usd.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

// ── Dates & times ─────────────────────────────────────────────────────────────

/**
 * Formats a date-only string (e.g. "2025-01-15") as a compact "Mon D" label,
 * e.g. "Jan 15". Intended for Recharts X-axis tick labels where space is limited
 * and the year is implied by context.
 *
 * Appends T00:00:00Z before parsing to force UTC midnight and avoid ±1-day
 * off-by-one errors from local-timezone interpretation.
 *
 * 'en-US' is intentional: chart axis labels are data, not prose — abbreviated
 * English month names ("Jan", "Feb") are universally readable in technical
 * contexts and keep label width predictable across locales.
 */
export function fmtDateAxis(dateStr: string): string {
  try {
    const d = new Date(dateStr + 'T00:00:00Z');
    return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
  } catch {
    return dateStr;
  }
}

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

/**
 * Formats a non-nullable ISO timestamp as a compact "date + time" string,
 * e.g. "Jan 5, 12:34 PM". Year is omitted — use for recent/current events
 * where the year is implied. Paired with fmtDate when year context is needed.
 */
export function fmtDateTime(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

// ── Tokens ────────────────────────────────────────────────────────────────────

/**
 * Compact token count with B/M/K suffix, e.g. 1.2B, 3.5M, 1.2K.
 * Use in billing stat cards and token breakdowns where the raw integer
 * would be too wide.
 */
export function fmtTokens(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

// ── Billing amounts ───────────────────────────────────────────────────────────

/**
 * Formats a USD billing amount to two decimal places, e.g. "$1,234.56".
 * Use for period/monthly/quarterly totals where sub-cent precision is not
 * meaningful. For per-request micro-costs use fmtCost instead.
 */
export function fmtUsd(usd: number | null | undefined): string {
  if (usd === null || usd === undefined || Number.isNaN(usd)) return '—';
  return `$${usd.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

/**
 * Suffix for a budget reset window: "weekly" → "/wk", anything else → "/mo".
 */
export function fmtWindowSuffix(cadence: string): string {
  return cadence === 'weekly' ? '/wk' : '/mo';
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
