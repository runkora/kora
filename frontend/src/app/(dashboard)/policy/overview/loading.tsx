import { Card, CardContent, CardHeader, CardTitle, Grid, Skeleton, Stack } from '@runkora/hades';

function KPISkeleton() {
  return (
    <Card column>
      <CardHeader compact>
        <CardTitle>
          <Skeleton shape="line" width="half" size="xs" />
        </CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="snug">
          <Skeleton shape="line" width="quarter" size="lg" />
          <Skeleton shape="line" width="three-quarters" size="xs" />
        </Stack>
      </CardContent>
    </Card>
  );
}

export default function PolicyOverviewLoading() {
  return (
    <Stack gap="comfortable">
      <Stack gap="snug">
        <Skeleton shape="line" width="quarter" size="lg" />
        <Skeleton shape="line" width="half" size="xs" />
      </Stack>
      <Grid cols={{ base: 1, sm: 2, lg: 4 }} gap="comfortable">
        <KPISkeleton />
        <KPISkeleton />
        <KPISkeleton />
        <KPISkeleton />
      </Grid>
      <Card column>
        <CardHeader compact>
          <CardTitle>
            <Skeleton shape="line" width="quarter" size="xs" />
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Stack gap="snug">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} shape="line" width="full" size="sm" />
            ))}
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
