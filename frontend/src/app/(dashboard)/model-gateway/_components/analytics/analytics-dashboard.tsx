'use client';

import { Callout, Grid, Heading, Stack, Text } from '@orbit/terra';
import type { AnalyticsDays, AnalyticsGranularity, AnalyticsResponse } from '../../_types';
import type { AnalyticsLoadStatus } from '../../_hooks/use-analytics';
import type { PeriodPrefKey } from '@orbit/components';
import { AnalyticsKPICards } from './analytics-kpi-cards';
import { AnalyticsSpendChart } from './analytics-spend-chart';
import { AnalyticsTokensChart } from './analytics-tokens-chart';
import { AnalyticsRequestsChart } from './analytics-requests-chart';
import { AnalyticsCostByModelChart } from './analytics-cost-by-model-chart';
import { AnalyticsLatencyChart } from './analytics-latency-chart';
import { AnalyticsPeriodControls } from './analytics-period-controls';
import { AnalyticsTokensByModelChart } from './analytics-tokens-by-model-chart';

// ── Types ─────────────────────────────────────────────────────────────────────

export interface AnalyticsDashboardProps {
  title: string;
  description: string;
  data: AnalyticsResponse | null;
  status: AnalyticsLoadStatus;
  error: string | null;
  days: AnalyticsDays;
  granularity: AnalyticsGranularity;
  periodPrefKey: PeriodPrefKey;
  onSetDays: (d: AnalyticsDays) => void;
  onSetGranularity: (g: AnalyticsGranularity) => void;
  /** Optional filter controls rendered between the description and the period controls. */
  filterSlot?: React.ReactNode;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function AnalyticsDashboard({
  title,
  description,
  data,
  status,
  error,
  days,
  granularity,
  periodPrefKey,
  onSetDays,
  onSetGranularity,
  filterSlot,
}: AnalyticsDashboardProps) {
  const buckets = data?.buckets ?? [];
  const costByModel = data?.cost_by_model ?? [];
  const tokensByModel = data?.tokens_by_model ?? [];
  const loading = status === 'loading';

  return (
    <Stack direction="vertical" gap="comfortable">
      {/* Title row */}
      <Stack gap="snug">
        <Heading variant="title">{title}</Heading>
        <Text size="supporting" color="muted">
          {description}
        </Text>
      </Stack>

      {/* Controls row — filter slot left, period controls right */}
      <Stack direction="horizontal" align="center" justify="between" gap="comfortable">
        {filterSlot ?? <Stack />}
        <AnalyticsPeriodControls
          days={days}
          granularity={granularity}
          periodPrefKey={periodPrefKey}
          onSetDays={onSetDays}
          onSetGranularity={onSetGranularity}
          loading={loading}
        />
      </Stack>

      {/* Error banner */}
      {error && (
        <Callout variant="error">
          <Text size="supporting" color="muted">
            {error}
          </Text>
        </Callout>
      )}

      {/* Data area — dims while reloading */}
      <Stack direction="vertical" gap="comfortable" asChild>
        <figure
          className={loading ? 'pointer-events-none opacity-50 transition-opacity' : undefined}
        >
          {/* KPI stat cards */}
          {data && <AnalyticsKPICards kpis={data.kpis} />}

          {/* Empty state when no bucket data */}
          {!error && data && buckets.length === 0 && (
            <Stack align="center" justify="center" gap="snug" asChild>
              <figure className="rounded-lg border border-dashed py-20 text-center">
                <Text color="muted">No inference activity yet for this period.</Text>
                <Text size="caption" color="subtle">
                  Data will appear here once requests are made through the gateway.
                </Text>
              </figure>
            </Stack>
          )}

          {/* Charts — only render when data is available */}
          {buckets.length > 0 && (
            <>
              <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
                <AnalyticsSpendChart buckets={buckets} />
                <AnalyticsTokensChart buckets={buckets} />
              </Grid>

              <AnalyticsRequestsChart buckets={buckets} />

              {tokensByModel.length > 0 && (
                <AnalyticsTokensByModelChart tokensByModel={tokensByModel} />
              )}

              <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
                <AnalyticsCostByModelChart costByModel={costByModel} />
                <AnalyticsLatencyChart buckets={buckets} />
              </Grid>
            </>
          )}
        </figure>
      </Stack>
    </Stack>
  );
}
