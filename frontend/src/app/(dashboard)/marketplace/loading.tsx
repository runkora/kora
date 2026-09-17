import { Skeleton, Stack } from '@orbit/terra';

export default function MarketplaceLoading() {
  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Skeleton shape="line" width="quarter" size="lg" />
        <Skeleton shape="line" width="half" size="xs" />
      </Stack>
      <Skeleton shape="block" width="full" size="xl" />
    </Stack>
  );
}
