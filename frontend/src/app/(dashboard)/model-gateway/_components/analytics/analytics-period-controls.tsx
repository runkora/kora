'use client';

import { NativeSelect, Stack } from '@orbit/terra';
import type { AnalyticsDays, AnalyticsGranularity } from '../../_types';

// ── Types ─────────────────────────────────────────────────────────────────────

interface AnalyticsPeriodControlsProps {
  days: AnalyticsDays;
  granularity: AnalyticsGranularity;
  onSetDays: (d: AnalyticsDays) => void;
  onSetGranularity: (g: AnalyticsGranularity) => void;
  loading?: boolean;
}

// ── Options ───────────────────────────────────────────────────────────────────

const DAYS_OPTIONS: { value: AnalyticsDays; label: string }[] = [
  { value: 7, label: 'Last 7 days' },
  { value: 30, label: 'Last 30 days' },
  { value: 90, label: 'Last 90 days' },
];

const GRANULARITY_OPTIONS: { value: AnalyticsGranularity; label: string }[] = [
  { value: 'daily', label: 'Daily' },
  { value: 'weekly', label: 'Weekly' },
  { value: 'monthly', label: 'Monthly' },
];

// ── Component ─────────────────────────────────────────────────────────────────

export function AnalyticsPeriodControls({
  days,
  granularity,
  onSetDays,
  onSetGranularity,
  loading = false,
}: AnalyticsPeriodControlsProps) {
  return (
    <Stack direction="horizontal" align="center" gap="snug">
      <NativeSelect
        value={days}
        disabled={loading}
        aria-label="Period"
        onChange={(e) => onSetDays(Number(e.target.value) as AnalyticsDays)}
      >
        {DAYS_OPTIONS.map(({ value, label }) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </NativeSelect>
      <NativeSelect
        value={granularity}
        disabled={loading}
        aria-label="Granularity"
        onChange={(e) => onSetGranularity(e.target.value as AnalyticsGranularity)}
      >
        {GRANULARITY_OPTIONS.map(({ value, label }) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </NativeSelect>
    </Stack>
  );
}

