import { Card, CardContent, Skeleton, Stack } from '@runkora/hades';

/** Stands in for the heading, the live control and the events table while the first page loads. */
export default function EventsLoading() {
  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Skeleton shape="line" width="quarter" size="lg" />
        <Skeleton shape="line" width="half" size="xs" />
      </Stack>
      <Stack direction="horizontal" justify="end">
        <Skeleton shape="block" width="quarter" size="sm" />
      </Stack>
      <Card column>
        <CardContent padded>
          <Stack gap="cozy">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} shape="block" width="full" size="lg" />
            ))}
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
