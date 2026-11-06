import { Skeleton, Stack } from '@runkora/hades';
import { SkillsResultsSkeleton } from './_components/skills-results-skeleton';

/** Stands in for the overview while the server loads the Marketplaces and the first page. */
export default function SkillsLoading() {
  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Skeleton shape="line" width="quarter" size="lg" />
        <Skeleton shape="line" width="half" size="xs" />
      </Stack>
      <Skeleton shape="block" width="full" size="md" />
      <SkillsResultsSkeleton />
    </Stack>
  );
}
