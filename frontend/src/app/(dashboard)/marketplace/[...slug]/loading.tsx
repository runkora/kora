import { Skeleton, Stack } from '@orbit/terra';

/**
 * The Skill detail route loads no data yet, so the skeleton stands in for a
 * title and a block of detail text. It commits to no final detail layout.
 */
export default function SkillDetailLoading() {
  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Skeleton shape="line" width="third" size="lg" />
        <Skeleton shape="line" width="two-thirds" size="xs" />
      </Stack>
      <Skeleton shape="block" width="full" size="xl" />
    </Stack>
  );
}
