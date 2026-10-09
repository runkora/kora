import { Card, CardContent, Grid, Skeleton, Stack } from '@orbit/hades';

/**
 * The MCP servers page fetches the tenant-visible server catalog in one server
 * pass, so the skeleton stands in for the whole surface: the header with its
 * build-config action, the controls row, and the server card grid beneath it.
 */
export default function McpServersLoading() {
  return (
    <Stack gap="comfortable">
      {/* Intro + primary CTA. */}
      <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
        <Stack gap="tight">
          <Skeleton shape="line" width="quarter" size="lg" />
          <Skeleton shape="line" width="half" size="xs" />
        </Stack>
        <Skeleton shape="block" width="quarter" size="sm" />
      </Stack>

      {/* Controls: search + filter left, count + view toggle right. */}
      <Stack direction="horizontal" align="center" justify="between" gap="comfortable">
        <Skeleton shape="block" width="third" size="md" />
        <Skeleton shape="block" width="quarter" size="md" />
      </Stack>

      {/* The server card grid. */}
      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
        {Array.from({ length: 4 }).map((_, i) => (
          <Card key={i} inset border="subtle">
            <CardContent padded>
              <Stack gap="snug">
                <Stack direction="horizontal" align="center" gap="snug">
                  <Skeleton shape="circle" size="lg" />
                  <Stack gap="tight" grow>
                    <Skeleton shape="line" width="half" size="sm" />
                    <Skeleton shape="line" width="third" size="xs" />
                  </Stack>
                </Stack>
                <Skeleton shape="line" width="full" size="xs" />
                <Stack direction="horizontal" gap="tight">
                  <Skeleton shape="block" width="quarter" size="sm" />
                  <Skeleton shape="block" width="quarter" size="sm" />
                  <Skeleton shape="block" width="quarter" size="sm" />
                </Stack>
              </Stack>
            </CardContent>
          </Card>
        ))}
      </Grid>
    </Stack>
  );
}
