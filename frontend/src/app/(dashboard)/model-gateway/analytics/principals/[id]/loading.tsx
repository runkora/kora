// loading.tsx — Suspense fallback for the per-principal usage drill-down.
//
// Mirrors the PrincipalUsageView layout (back link, identity header, KPI
// strip, chart) so there is no layout shift when data arrives.

import { Card, CardContent, CardHeader, Grid, Skeleton, Stack } from '@clear-route/hades';

function KPICardSkeleton() {
  return (
    <Card inset border="subtle">
      <CardContent padded compact>
        <Stack gap="tight">
          <Skeleton shape="line" width="half" size="xs" />
          <Skeleton shape="line" width="quarter" size="lg" />
        </Stack>
      </CardContent>
    </Card>
  );
}

export default function PrincipalUsageLoading() {
  return (
    <Stack gap="comfortable">
      {/* Back-to-analytics link */}
      <Stack direction="horizontal">
        <Skeleton shape="block" width="quarter" size="md" />
      </Stack>

      {/* Identity header (name + email) + period controls */}
      <Stack direction="horizontal" align="start" justify="between">
        <Stack gap="tight">
          <Skeleton shape="line" width="third" size="lg" />
          <Skeleton shape="line" width="half" size="xs" />
        </Stack>
        <Skeleton shape="block" width="quarter" size="sm" />
      </Stack>

      {/* KPI strip */}
      <Grid cols={{ base: 2, lg: 4 }} gap="snug">
        {Array.from({ length: 4 }).map((_, i) => (
          <KPICardSkeleton key={i} />
        ))}
      </Grid>

      {/* Usage chart */}
      <Card column>
        <CardHeader compact>
          <Skeleton shape="line" width="third" size="xs" />
        </CardHeader>
        <CardContent>
          <Skeleton shape="block" width="full" size="xl" />
        </CardContent>
      </Card>
    </Stack>
  );
}
