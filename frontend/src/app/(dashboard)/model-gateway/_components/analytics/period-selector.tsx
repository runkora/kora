'use client';

/**
 * @tier 3 — analytics-scoped. Reads and writes `preferences.analytics.defaultPeriod`
 * or `preferences.myUsage.defaultPeriod` via `usePreferences()`.
 *
 * USAGE SCOPE: Requires `<PreferencesProvider>` in the ancestor tree, which is
 * only mounted in `(dashboard)/layout.tsx`. Do not use this component outside
 * the authenticated dashboard shell — `usePreferences()` will throw.
 *
 * Wraps the 7d / 30d / 90d period buttons. `prefKey` identifies which preference
 * key to persist. `value` + `onChange` are controlled by the analytics context
 * and drive data re-fetching — PeriodSelector intercepts `onChange` to also persist.
 */

import { Button, Stack } from '@clear-route/hades';
import { usePreferences } from '../../../_preferences/preferences-context';
import { toPeriod } from '../../../_preferences/period';
import type { AnalyticsDays, PeriodPrefKey } from '../../../_preferences/period';

export type { PeriodPrefKey };

// ── Types ─────────────────────────────────────────────────────────────────────

export interface PeriodSelectorProps {
  /** Preference key to read and write (e.g. `"analytics.defaultPeriod"`). */
  prefKey: PeriodPrefKey;
  /** Currently active period — controlled by the analytics context. */
  value: AnalyticsDays;
  /** Called when the user selects a period. Also persists the new value to preferences. */
  onChange: (days: AnalyticsDays) => void;
  loading?: boolean;
}

// ── Component ─────────────────────────────────────────────────────────────────

const DAYS_OPTIONS: { value: AnalyticsDays; label: string }[] = [
  { value: 7, label: '7d' },
  { value: 30, label: '30d' },
  { value: 90, label: '90d' },
];

export function PeriodSelector({ prefKey, value, onChange, loading = false }: PeriodSelectorProps) {
  const { updatePreferences } = usePreferences();
  const section = prefKey === 'analytics.defaultPeriod' ? 'analytics' : 'myUsage';

  function handleChange(days: AnalyticsDays) {
    onChange(days);
    updatePreferences((prev) => ({
      ...prev,
      [section]: { defaultPeriod: toPeriod(days) },
    }));
  }

  return (
    <Stack direction="horizontal" gap="tight">
      {DAYS_OPTIONS.map(({ value: opt, label }) => (
        <Button
          key={opt}
          variant={value === opt ? 'default' : 'ghost'}
          size="xs"
          disabled={loading}
          onClick={() => handleChange(opt)}
        >
          {label}
        </Button>
      ))}
    </Stack>
  );
}
