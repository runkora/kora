import { Skeleton, Stack } from '@runkora/hades';

/** Stands in for the Runs page header and its first rows while the route loads. */
export default function RunsLoading() {
  return (
    <Stack gap="relaxed">
      <Stack direction="horizontal" align="center" justify="between">
        <Stack gap="tight">
          <Skeleton shape="line" width="quarter" size="lg" />
          <Skeleton shape="line" width="half" size="xs" />
        </Stack>
        <Skeleton shape="block" width="quarter" size="sm" />
      </Stack>
      <Stack gap="cozy">
        {Array.from({ length: 8 }, (_, i) => (
          <Skeleton key={i} shape="block" width="full" size="lg" />
        ))}
      </Stack>
    </Stack>
  );
}
