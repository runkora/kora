'use client';

import { Text } from '@orbit/terra';

// ── Types ─────────────────────────────────────────────────────────────────────

export type Granularity = 'daily' | 'weekly' | 'monthly';
export type Period = '7d' | '14d' | '30d' | '90d' | '180d' | '365d';

interface TableControlsProps {
  granularity: Granularity;
  period: Period;
  onGranularityChange: (g: Granularity) => void;
  onPeriodChange: (p: Period) => void;
}

// ── Inline select ─────────────────────────────────────────────────────────────

interface InlineSelectProps<T extends string> {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (v: T) => void;
}

function InlineSelect<T extends string>({ label, value, options, onChange }: InlineSelectProps<T>) {
  return (
    <div className="flex items-center gap-1.5">
      <Text size="caption" color="subtle" asChild>
        <label>{label}</label>
      </Text>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value as T)}
        className="h-7 rounded-md border border-border bg-surface-elevated px-2 text-xs text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent cursor-pointer"
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
    </div>
  );
}

// ── TableControls ─────────────────────────────────────────────────────────────

const GRANULARITY_OPTIONS: { value: Granularity; label: string }[] = [
  { value: 'daily', label: 'Daily' },
  { value: 'weekly', label: 'Weekly' },
  { value: 'monthly', label: 'Monthly' },
];

const PERIOD_OPTIONS: { value: Period; label: string }[] = [
  { value: '7d', label: 'Last 7 days' },
  { value: '14d', label: 'Last 14 days' },
  { value: '30d', label: 'Last 30 days' },
  { value: '90d', label: 'Last 90 days' },
  { value: '180d', label: 'Last 180 days' },
  { value: '365d', label: 'Last year' },
];

export function TableControls({ granularity, period, onGranularityChange, onPeriodChange }: TableControlsProps) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      <InlineSelect
        label="Granularity"
        value={granularity}
        options={GRANULARITY_OPTIONS}
        onChange={onGranularityChange}
      />
      <InlineSelect
        label="Period"
        value={period}
        options={PERIOD_OPTIONS}
        onChange={onPeriodChange}
      />
    </div>
  );
}
