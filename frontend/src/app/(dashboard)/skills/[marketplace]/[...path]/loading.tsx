import { Card, CardContent, CardHeader, Skeleton, Stack } from '@runkora/hades';

/** Mirrors the skill page: the header, the open file and the Files and Source rail. */
export default function SkillLoading() {
  const rail = [3, 5].map((rows) => (
    <Card key={rows} column>
      <CardHeader>
        <Skeleton shape="line" width="third" size="sm" />
      </CardHeader>
      <CardContent>
        <Stack gap="snug">
          {Array.from({ length: rows }).map((_, i) => (
            <Skeleton key={i} shape="line" width="three-quarters" size="sm" />
          ))}
        </Stack>
      </CardContent>
    </Card>
  ));

  return (
    <Stack gap="relaxed" role="status" aria-label="Loading the skill">
      <Stack gap="snug">
        <Skeleton shape="line" width="third" size="lg" />
        <Skeleton shape="line" width="half" size="sm" />
      </Stack>
      <Stack direction={{ base: 'vertical', xl: 'horizontal' }} gap="relaxed">
        <Stack grow minWidth="none">
          <Skeleton shape="block" width="full" size="xl" />
        </Stack>
        <Stack
          gap="comfortable"
          minWidth={{ base: 'none', xl: 'detail-panel' }}
          maxWidth={{ base: 'full', xl: 'detail-panel' }}
        >
          {rail}
        </Stack>
      </Stack>
    </Stack>
  );
}
