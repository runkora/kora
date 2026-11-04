import { Card, CardContent, CardHeader, Skeleton, Stack } from '@runkora/hades';

/** Mirrors the skill page: file explorer and preview beside the repository card. */
export default function SkillLoading() {
  return (
    <Stack gap="comfortable" role="status" aria-label="Loading the skill">
      <Skeleton shape="line" width="quarter" size="sm" />
      <Stack direction={{ base: 'vertical', lg: 'horizontal' }} gap="relaxed">
        <Stack gap="comfortable" grow minWidth="none">
          <Card column>
            <CardHeader>
              <Skeleton shape="line" width="third" size="sm" />
            </CardHeader>
            <CardContent>
              <Stack gap="snug">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} shape="line" width="half" size="sm" />
                ))}
              </Stack>
            </CardContent>
          </Card>
          <Skeleton shape="block" width="full" size="xl" />
        </Stack>
        <Stack
          minWidth={{ base: 'none', lg: 'detail-panel' }}
          maxWidth={{ base: 'full', lg: 'detail-panel' }}
        >
          <Card column>
            <CardContent padded>
              <Stack gap="tight">
                <Skeleton shape="line" width="half" size="sm" />
                <Skeleton shape="line" width="three-quarters" size="xs" />
              </Stack>
            </CardContent>
          </Card>
        </Stack>
      </Stack>
    </Stack>
  );
}
