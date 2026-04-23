'use client';

import { Grid, Heading, Stack, Text } from '@orbit/terra';
import type { AnalyticsDays, AnalyticsGranularity, AnalyticsResponse } from '../../_types';
import type { AnalyticsLoadStatus } from '../../_hooks/use-analytics';
import { AnalyticsKPICards } from './analytics-kpi-cards';
import { AnalyticsSpendChart } from './analytics-spend-chart';
import { AnalyticsTokensChart } from './analytics-tokens-chart';
import { AnalyticsRequestsChart } from './analytics-requests-chart';
import { AnalyticsCostByModelChart } from './analytics-cost-by-model-chart';
import { AnalyticsLatencyChart } from './analytics-latency-chart';
import { AnalyticsPeriodControls } from './analytics-period-controls';
import { AnalyticsTokensByModelChart } from './analytics-tokens-by-model-chart';

// ── Empty / loading states ────────────────────────────────────────────────────

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed py-20 text-center">
      <Text color="muted">No inference activity yet for this period.</Text>
      <Text size="caption" color="subtle">
        Data will appear here once requests are made through the gateway.
      </Text>
    </div>
  );
}

function ErrorState({ message }: { message: string }) {
  return (
    <div className="rounded-lg border border-status-error/20 bg-status-error/5 px-4 py-3">
      <Text size="supporting" color="muted">
        {message}
      </Text>
    </div>
  );
}

// ── Types ─────────────────────────────────────────────────────────────────────

export interface AnalyticsDashboardProps {
  title: string;
  description: string;
  data: AnalyticsResponse | null;
  status: AnalyticsLoadStatus;
  error: string | null;
  days: AnalyticsDays;
  granularity: AnalyticsGranularity;
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
  onSetDays,
  onSetGranularity,
  filterSlot,
}: AnalyticsDashboardProps) {
  const buckets = data?.buckets ?? [];
  const costByModel = data?.cost_by_model ?? [];
  const tokensByModel = data?.tokens_by_model ?? [];
  const loading = status === 'loading';

  return (
    <Stack direction="vertical" gap="comfortable" asChild>
      <section>
        {/* Title row */}
        <Stack gap="snug">
          <Heading variant="title">{title}</Heading>
          <Text size="supporting" color="muted">
            {description}
          </Text>
        </Stack>

        {/* Controls row — filter slot left, period controls right */}
        <Stack direction="horizontal" align="center" justify="between" gap="comfortable">
          <span>{filterSlot}</span>
          <AnalyticsPeriodControls
            days={days}
            granularity={granularity}
            onSetDays={onSetDays}
            onSetGranularity={onSetGranularity}
            loading={loading}
          />
        </Stack>

        {/* Error banner */}
        {error && <ErrorState message={error} />}

        {/* Data area — dims while reloading */}
        <section
          className={loading ? 'pointer-events-none opacity-50 transition-opacity' : undefined}
        >
          <Stack direction="vertical" gap="comfortable">
            {/* KPI stat cards */}
            {data ? <AnalyticsKPICards kpis={data.kpis} /> : <div className="h-[120px]" />}

            {/* Empty state when no bucket data */}
            {!error && data && buckets.length === 0 && <EmptyState />}

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
          </Stack>
        </section>
      </section>
    </Stack>
  );
}
