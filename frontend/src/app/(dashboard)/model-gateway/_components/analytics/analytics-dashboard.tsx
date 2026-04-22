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
}: AnalyticsDashboardProps) {
  const buckets = data?.buckets ?? [];
  const costByModel = data?.cost_by_model ?? [];

  return (
    <Stack direction="vertical" gap="comfortable" asChild>
      <section>
        {/* Header + period controls */}
        <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
          <Stack gap="snug">
            <Heading variant="title">{title}</Heading>
            <Text size="supporting" color="muted">
              {description}
            </Text>
          </Stack>
          <AnalyticsPeriodControls
            days={days}
            granularity={granularity}
            onSetDays={onSetDays}
            onSetGranularity={onSetGranularity}
            loading={status === 'loading'}
          />
        </Stack>

        {/* Error banner */}
        {error && <ErrorState message={error} />}

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

            <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
              <AnalyticsCostByModelChart costByModel={costByModel} />
              <AnalyticsLatencyChart buckets={buckets} />
            </Grid>
          </>
        )}
      </section>
    </Stack>
  );
}
