/** @tier 3 — budgets-page-scoped. Status and budget-source formatting shared
 * by the principals table and its row-click detail modal. */

import { fmtUsd, fmtWindowSuffix } from '@runkora/utils';
import type { BudgetCategoryStatus, BudgetPrincipalRow } from '../../_types';

export const NEAR_LIMIT_THRESHOLD = 0.8;

// utilisation compares effective_spend_usd (window-matched to the effective
// limit) against the limit. Using current_spend_usd (always monthly) against a
// weekly effective limit would inflate the ratio by 4-5x for weekly principals.
export function utilisation(p: BudgetPrincipalRow): number {
  if (!p.has_limit || p.effective_limit_usd <= 0) return 0;
  return p.effective_spend_usd / p.effective_limit_usd;
}

export type BudgetStatusVariant = 'idle' | 'degraded' | 'healthy' | 'error';

export function principalStatus(p: BudgetPrincipalRow): {
  variant: BudgetStatusVariant;
  label: string;
} {
  if (!p.has_limit) return { variant: 'idle', label: 'No budget' };
  if (p.spend_limit_reached) return { variant: 'error', label: 'Blocked' };
  if (utilisation(p) >= NEAR_LIMIT_THRESHOLD) return { variant: 'degraded', label: 'Near limit' };
  return { variant: 'healthy', label: 'OK' };
}

// Badge text never wraps, so an unbounded id would overflow its column.
const MAX_CHIP_NAME_LENGTH = 12;

function truncateWithEllipsis(value: string, maxLength: number): string {
  return value.length > maxLength ? `${value.slice(0, maxLength)}…` : value;
}

// Renders as just type:name, e.g. "role:engineer" or "grant:e6b4b08d-e5…".
export function chipLabel(cat: BudgetCategoryStatus): string {
  return `${cat.source}:${truncateWithEllipsis(cat.id, MAX_CHIP_NAME_LENGTH)}`;
}

// Full id plus the spend/limit, per-run cap, and pool marker that used to be
// always-visible text on the chip before truncation — shown on hover instead,
// so the table stays compact but the numbers are still one hover away instead
// of a full trip into the detail modal.
export function chipTitle(cat: BudgetCategoryStatus): string {
  const parts = [`${cat.source}:${cat.id}`];
  if (cat.limit_usd > 0) {
    parts.push(
      `${fmtUsd(cat.spend_usd)} / ${fmtUsd(cat.limit_usd)}${fmtWindowSuffix(cat.reset_cadence)}`,
    );
  }
  if (cat.per_run_limit_usd) {
    parts.push(`${fmtUsd(cat.per_run_limit_usd)}/run`);
  }
  if (cat.pooled) {
    parts.push('pooled');
  }
  return parts.join(' · ');
}

// Chip colour for a single source, independent of the principal's overall
// status — lets one degraded source stand out among otherwise-healthy ones
// without adding any text.
export function chipVariant(cat: BudgetCategoryStatus): 'elevated' | 'degraded' | 'error' {
  if (cat.limit_usd <= 0) return 'elevated';
  if (cat.spend_usd >= cat.limit_usd) return 'error';
  if (cat.spend_usd / cat.limit_usd >= NEAR_LIMIT_THRESHOLD) return 'degraded';
  return 'elevated';
}
