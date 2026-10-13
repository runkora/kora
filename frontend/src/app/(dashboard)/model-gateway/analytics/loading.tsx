// loading.tsx — Suspense fallback for the tenant-wide analytics page.
//
// Mirrors the AnalyticsShell layout (tab strip, then the dashboard: header,
// KPI strip, charts) so there is no layout shift when data arrives.

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

export default function AnalyticsLoading() {
  return (
    <Stack gap="comfortable">
      {/* Tab strip: Overview / Principals */}
      <Stack direction="horizontal" gap="snug">
        <Skeleton shape="block" width="quarter" size="md" />
      </Stack>

      {/* Dashboard header + period controls */}
      <Stack direction="horizontal" align="start" justify="between">
        <Stack gap="tight">
          <Skeleton shape="line" width="quarter" size="lg" />
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

      {/* Charts */}
      <Card column>
        <CardHeader compact>
          <Skeleton shape="line" width="third" size="xs" />
        </CardHeader>
        <CardContent>
          <Skeleton shape="block" width="full" size="xl" />
        </CardContent>
      </Card>
      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
        {Array.from({ length: 2 }).map((_, i) => (
          <Card key={i} column>
            <CardHeader compact>
              <Skeleton shape="line" width="third" size="xs" />
            </CardHeader>
            <CardContent>
              <Skeleton shape="block" width="full" size="xl" />
            </CardContent>
          </Card>
        ))}
      </Grid>
    </Stack>
  );
}
