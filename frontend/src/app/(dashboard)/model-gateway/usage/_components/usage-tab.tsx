'use client';

import { Grid, Heading, Stack, Text } from '@clear-route/hades';
import { StatCards } from './stat-cards';
import { SpendChart } from './spend-chart';
import { TokensChart } from './tokens-chart';
import { RequestsChart } from './requests-chart';
import { CostByModelChart } from './cost-by-model-chart';
import { LatencyChart } from './latency-chart';
import { STATS } from '../../_data/mock';

export function UsageTab() {
  return (
    <Stack direction="vertical" gap="comfortable">
      {/* Page header */}
      <Stack gap="snug">
        <Heading variant="title">Usage</Heading>
        <Text size="supporting" color="muted">
          Detailed analytics for spend, tokens, and request volume across your inference gateway.
        </Text>
      </Stack>

      {/* KPI stat cards */}
      <StatCards stats={STATS} />

      {/* Spend and Tokens charts */}
      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
        <SpendChart />
        <TokensChart />
      </Grid>

      {/* Requests chart — full width */}
      <RequestsChart />

      {/* Cost by model and Latency charts */}
      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
        <CostByModelChart />
        <LatencyChart />
      </Grid>
    </Stack>
  );
}
