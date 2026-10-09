import { Card, CardContent, CardHeader, CardTitle, Skeleton, Stack } from '@orbit/hades';

export default function EvaluateLoading() {
  return (
    <Stack gap="comfortable">
      <Skeleton shape="line" width="quarter" size="lg" />
      <Card column>
        <CardHeader compact>
          <CardTitle>
            <Skeleton shape="line" width="half" size="xs" />
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Stack gap="snug">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} shape="line" width="full" size="sm" />
            ))}
          </Stack>
        </CardContent>
      </Card>
      <Card column>
        <CardHeader compact>
          <CardTitle>
            <Skeleton shape="line" width="half" size="xs" />
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Stack gap="snug">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} shape="line" width="three-quarters" size="sm" />
            ))}
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
