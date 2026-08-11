// Suspense fallback for the models tab.
//
// Simple skeleton standing in for the DataTable while data loads.

import { Badge, Card, CardContent, Skeleton, Stack } from '@orbit/terra';

export default function ModelsLoading() {
  return (
    <Card column>
      <CardContent flush padded>
        <Stack gap="snug">
          {/* Toolbar skeleton */}
          <Stack direction="horizontal" align="center" justify="between" gap="comfortable">
            <Skeleton shape="block" width="quarter" size="sm" />
            <Stack direction="horizontal" align="center" gap="comfortable">
              <Skeleton shape="line" width="quarter" size="xs" />
              <Skeleton shape="block" width="quarter" size="sm" />
            </Stack>
          </Stack>

          {/* Row skeletons */}
          {Array.from({ length: 12 }).map((_, i) => (
            <Stack key={i} direction="horizontal" align="center" gap="comfortable">
              <Skeleton shape="circle" size="xs" />
              <Stack gap="none" grow>
                <Skeleton shape="line" width="three-quarters" size="sm" />
                <Skeleton shape="line" width="full" size="xs" />
              </Stack>
              <Skeleton shape="line" width="half" size="sm" />
              <Skeleton shape="circle" size="xs" />
              <Stack direction="horizontal" gap="tight">
                <Badge variant="outline" size="sm">
                  <Skeleton shape="line" width="full" size="xs" />
                </Badge>
              </Stack>
              <Skeleton shape="circle" size="xs" />
            </Stack>
          ))}
        </Stack>
      </CardContent>
    </Card>
  );
}
