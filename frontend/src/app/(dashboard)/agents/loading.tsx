import { Card, CardContent, CardFooter, CardHeader, Grid, Skeleton, Stack } from '@orbit/terra';

/**
 * The Agents page loads agent types and recent runs in one server pass, so the
 * skeleton stands in for the whole surface: the header, the fleet KPI strip,
 * and the grid of agent type cards beneath it.
 */
export default function AgentsLoading() {
  return (
    <Stack gap="comfortable">
      <Stack direction="horizontal" align="center" justify="between">
        <Stack gap="tight">
          <Skeleton shape="line" width="quarter" size="lg" />
          <Skeleton shape="line" width="half" size="xs" />
        </Stack>
        <Skeleton shape="block" width="quarter" size="sm" />
      </Stack>

      {/* The fleet KPI strip. */}
      <Grid cols={{ base: 2, sm: 3, lg: 5 }} gap="comfortable">
        {Array.from({ length: 5 }).map((_, i) => (
          <Card key={i} inset border="subtle">
            <CardContent padded compact>
              <Stack gap="tight">
                <Skeleton shape="line" width="half" size="xs" />
                <Skeleton shape="line" width="quarter" size="lg" />
              </Stack>
            </CardContent>
          </Card>
        ))}
      </Grid>

      {/* The agent type card grid. */}
      <Grid cols={{ base: 1, sm: 2, lg: 3 }} gap="comfortable">
        {Array.from({ length: 6 }).map((_, i) => (
          <Card key={i} column>
            <CardHeader compact>
              <Stack direction="horizontal" align="center" justify="between">
                <Stack gap="tight">
                  <Skeleton shape="line" width="half" size="sm" />
                  <Skeleton shape="line" width="third" size="xs" />
                </Stack>
                <Skeleton shape="circle" size="md" />
              </Stack>
            </CardHeader>
            <CardContent padded grow>
              <Stack gap="snug">
                <Skeleton shape="line" width="full" size="xs" />
                <Skeleton shape="line" width="three-quarters" size="xs" />
              </Stack>
            </CardContent>
            <CardFooter compact justify="between">
              <Skeleton shape="line" width="third" size="xs" />
              <Skeleton shape="block" width="quarter" size="sm" />
            </CardFooter>
          </Card>
        ))}
      </Grid>
    </Stack>
  );
}
