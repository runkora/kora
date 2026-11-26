import { Card, CardContent, Skeleton, Stack } from '@runkora/hades';

/** Stands in for the Runs page header and its first rows while the route loads. */
export default function RunsLoading() {
  return (
    <Stack gap="comfortable">
      <Stack direction="horizontal" align="center" justify="between">
        <Stack gap="tight">
          <Skeleton shape="line" width="quarter" size="lg" />
          <Skeleton shape="line" width="half" size="xs" />
        </Stack>
        <Skeleton shape="block" width="quarter" size="sm" />
      </Stack>
      <Card column>
        <CardContent padded>
          <Stack gap="cozy">
            {Array.from({ length: 8 }, (_, i) => (
              <Skeleton key={i} shape="block" width="full" size="lg" />
            ))}
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
