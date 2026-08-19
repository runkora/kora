'use client';

/** @tier 3 — model-gateway logs-scoped. The server-side filter bar for the Logs
 *  table (status / principal / model / time window). Promote to src/components/
 *  only if a second feature needs the same NativeSelect filter-bar shape. */

import { NativeSelect, Stack } from '@orbit/terra';
import type { LogsTimeRange } from '../_hooks/use-logs';

// ── Options ─────────────────────────────────────────────────────────────────

export interface FilterOption {
  value: string;
  label: string;
}

const TIME_RANGE_OPTIONS: { value: LogsTimeRange; label: string }[] = [
  { value: 'all', label: 'All time' },
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
];

// ── Inline filter select ──────────────────────────────────────────────────────
//
// A NativeSelect whose empty value ('') is the "all" sentinel. onSelect receives
// null for the all option and the raw value otherwise, so callers never special-
// case the empty string.

interface FilterSelectProps {
  ariaLabel: string;
  /** Label for the all/unfiltered option, e.g. "All models". */
  allLabel: string;
  value: string | null;
  options: FilterOption[];
  disabled?: boolean;
  onSelect: (value: string | null) => void;
}

function FilterSelect({ ariaLabel, allLabel, value, options, disabled, onSelect }: FilterSelectProps) {
  // NativeSelect is w-full by default; the wrapping Stack bounds it to a compact
  // toolbar width (matching the filter-rows-editor precedent) — width lives on
  // the layout element, never as className on the Terra component.
  return (
    <Stack minWidth="form-field-sm" maxWidth="form-field-sm">
      <NativeSelect
        aria-label={ariaLabel}
        value={value ?? ''}
        disabled={disabled}
        onChange={(e) => onSelect(e.target.value === '' ? null : e.target.value)}
      >
        <option value="">{allLabel}</option>
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </NativeSelect>
    </Stack>
  );
}

// ── LogsToolbar ─────────────────────────────────────────────────────────────

export interface LogsToolbarProps {
  disabled?: boolean;

  statusValue: string | null;
  statusOptions: FilterOption[];
  onStatusChange: (value: string | null) => void;

  /** Admin 'all' view only. When false, the principal control is not rendered. */
  showPrincipal?: boolean;
  principalValue: string | null;
  principalOptions: FilterOption[];
  onPrincipalChange: (value: string | null) => void;

  modelValue: string | null;
  modelOptions: FilterOption[];
  onModelChange: (value: string | null) => void;

  timeRange: LogsTimeRange;
  onTimeRangeChange: (value: LogsTimeRange) => void;
}

/**
 * LogsToolbar
 *
 * The filter row for the Logs table, rendered into the DataTable `quickFilters`
 * slot. Every control drives a server-side refetch (via the useLogs setters) so
 * filtering spans all pages, not just the loaded one — matching how the
 * Principal filter already behaved. Principal is admin-only.
 */
export function LogsToolbar({
  disabled,
  statusValue,
  statusOptions,
  onStatusChange,
  showPrincipal = false,
  principalValue,
  principalOptions,
  onPrincipalChange,
  modelValue,
  modelOptions,
  onModelChange,
  timeRange,
  onTimeRangeChange,
}: LogsToolbarProps) {
  return (
    <Stack direction="horizontal" align="center" gap="tight" wrap>
      <FilterSelect
        ariaLabel="Filter by status"
        allLabel="All statuses"
        value={statusValue}
        options={statusOptions}
        disabled={disabled}
        onSelect={onStatusChange}
      />

      {showPrincipal && (
        <FilterSelect
          ariaLabel="Filter by principal"
          allLabel="All principals"
          value={principalValue}
          options={principalOptions}
          disabled={disabled}
          onSelect={onPrincipalChange}
        />
      )}

      <FilterSelect
        ariaLabel="Filter by model"
        allLabel="All models"
        value={modelValue}
        options={modelOptions}
        disabled={disabled}
        onSelect={onModelChange}
      />

      <Stack minWidth="form-field-sm" maxWidth="form-field-sm">
        <NativeSelect
          aria-label="Filter by time range"
          value={timeRange}
          disabled={disabled}
          onChange={(e) => onTimeRangeChange(e.target.value as LogsTimeRange)}
        >
          {TIME_RANGE_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </NativeSelect>
      </Stack>
    </Stack>
  );
}
