'use client';

import { Button, Stack, Text } from '@orbit/terra';
import { PeriodSelector } from './period-selector';
import type { PeriodPrefKey } from '../../../_preferences/period';
import type { AnalyticsDays, AnalyticsGranularity } from '../../_types';

// ── Types ─────────────────────────────────────────────────────────────────────

interface AnalyticsPeriodControlsProps {
  days: AnalyticsDays;
  granularity: AnalyticsGranularity;
  periodPrefKey: PeriodPrefKey;
  onSetDays: (d: AnalyticsDays) => void;
  onSetGranularity: (g: AnalyticsGranularity) => void;
  loading?: boolean;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

const GRANULARITY_OPTIONS: { value: AnalyticsGranularity; label: string }[] = [
  { value: 'daily', label: 'Daily' },
  { value: 'weekly', label: 'Weekly' },
  { value: 'monthly', label: 'Monthly' },
];

const MONTH_ABBR = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
];

function formatDate(d: Date): string {
  return `${MONTH_ABBR[d.getMonth()]} ${d.getDate()}`;
}

function computeDateRange(days: number): string {
  const now = new Date();
  const from = new Date(now);
  from.setDate(from.getDate() - days);
  return `${formatDate(from)} – ${formatDate(now)}`;
}

function ControlButton({
  active,
  disabled,
  onClick,
  children,
}: {
  active: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Button variant={active ? 'default' : 'ghost'} size="xs" onClick={onClick} disabled={disabled}>
      {children}
    </Button>
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

export function AnalyticsPeriodControls({
  days,
  granularity,
  periodPrefKey,
  onSetDays,
  onSetGranularity,
  loading = false,
}: AnalyticsPeriodControlsProps) {
  return (
    <Stack direction="horizontal" align="center" gap="comfortable">
      <Text size="caption" color="subtle">
        {computeDateRange(days)}
      </Text>
      <PeriodSelector prefKey={periodPrefKey} value={days} onChange={onSetDays} loading={loading} />
      <Stack direction="horizontal" gap="tight">
        {GRANULARITY_OPTIONS.map(({ value, label }) => (
          <ControlButton
            key={value}
            active={granularity === value}
            disabled={loading}
            onClick={() => onSetGranularity(value)}
          >
            {label}
          </ControlButton>
        ))}
      </Stack>
    </Stack>
  );
}
