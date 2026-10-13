import { Card, CardContent, CardHeader, Grid, Skeleton, Stack } from '@clear-route/hades';

/**
 * The agent detail page loads the agent type, its runs and its budget in one
 * server pass, so the skeleton stands in for the whole surface: the identity
 * header, the tab strip, and the overview cards beneath it.
 */
export default function AgentDetailLoading() {
  return (
    <Stack gap="comfortable">
      {/* The identity header. */}
      <Stack direction="horizontal" align="center" justify="between">
        <Stack direction="horizontal" align="start" gap="cozy">
          <Skeleton shape="circle" size="lg" />
          <Stack gap="tight">
            <Skeleton shape="line" width="half" size="lg" />
            <Skeleton shape="line" width="three-quarters" size="xs" />
          </Stack>
        </Stack>
        <Skeleton shape="block" width="quarter" size="sm" />
      </Stack>

      {/* The tab strip. */}
      <Stack direction="horizontal" gap="snug">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} shape="block" width="quarter" size="md" />
        ))}
      </Stack>

      {/* The overview cards. */}
      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
        {Array.from({ length: 2 }).map((_, i) => (
          <Card key={i} column>
            <CardHeader compact>
              <Skeleton shape="line" width="quarter" size="sm" />
            </CardHeader>
            <CardContent>
              <Stack gap="snug">
                <Skeleton shape="line" width="full" size="sm" />
                <Skeleton shape="line" width="three-quarters" size="sm" />
                <Skeleton shape="line" width="full" size="sm" />
                <Skeleton shape="line" width="half" size="sm" />
              </Stack>
            </CardContent>
          </Card>
        ))}
      </Grid>
    </Stack>
  );
}
