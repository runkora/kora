/** @tier 3 — skills-scoped. Skeleton of the skills table and the pagination bar. */

import { Card, CardContent, Skeleton, Stack } from '@runkora/hades';

export function SkillsResultsSkeleton() {
  return (
    <Stack gap="comfortable" role="status" aria-label="Loading skills">
      <Card column>
        <CardContent padded>
          <Stack gap="cozy">
            {Array.from({ length: 8 }).map((_, i) => (
              <Stack key={i} direction="horizontal" gap="comfortable">
                <Skeleton shape="line" width="quarter" size="sm" />
                <Skeleton shape="line" width="half" size="sm" />
              </Stack>
            ))}
          </Stack>
        </CardContent>
      </Card>
      <Stack direction="horizontal" align="center" justify="between" gap="comfortable">
        <Skeleton shape="line" width="quarter" size="sm" />
        <Skeleton shape="block" width="third" size="md" />
      </Stack>
    </Stack>
  );
}
