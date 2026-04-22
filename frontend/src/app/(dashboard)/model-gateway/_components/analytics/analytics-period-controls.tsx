'use client';

import { Stack } from '@orbit/terra';
import type { AnalyticsDays, AnalyticsGranularity } from '../../_types';

// ── Types ─────────────────────────────────────────────────────────────────────

interface AnalyticsPeriodControlsProps {
  days: AnalyticsDays;
  granularity: AnalyticsGranularity;
  onSetDays: (d: AnalyticsDays) => void;
  onSetGranularity: (g: AnalyticsGranularity) => void;
  loading?: boolean;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

const DAYS_OPTIONS: { value: AnalyticsDays; label: string }[] = [
  { value: 7, label: '7d' },
  { value: 30, label: '30d' },
  { value: 90, label: '90d' },
];

const GRANULARITY_OPTIONS: { value: AnalyticsGranularity; label: string }[] = [
  { value: 'daily', label: 'Daily' },
  { value: 'weekly', label: 'Weekly' },
  { value: 'monthly', label: 'Monthly' },
];

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
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      style={{
        padding: '3px 10px',
        borderRadius: '4px',
        fontSize: '12px',
        border: active ? '1px solid #e8a07a' : '1px solid #44444d',
        background: active ? 'rgba(232,160,122,0.15)' : 'transparent',
        color: active ? '#e8a07a' : disabled ? '#55555f' : '#8a8a94',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.6 : 1,
        transition: 'border-color 0.15s, color 0.15s',
      }}
    >
      {children}
    </button>
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

export function AnalyticsPeriodControls({
  days,
  granularity,
  onSetDays,
  onSetGranularity,
  loading = false,
}: AnalyticsPeriodControlsProps) {
  return (
    <Stack direction="horizontal" align="center" gap="comfortable">
      <Stack direction="horizontal" gap="tight">
        {DAYS_OPTIONS.map(({ value, label }) => (
          <ControlButton
            key={value}
            active={days === value}
            disabled={loading}
            onClick={() => onSetDays(value)}
          >
            {label}
          </ControlButton>
        ))}
      </Stack>
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
