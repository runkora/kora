// Suspense fallback for the logs tab.
//
// Simple skeleton standing in for the DataTable while data loads. Also serves
// as the Suspense boundary required by the client-side useSearchParams() call
// inside LogsTab.

import { Card, CardContent, Skeleton, Stack } from '@runkora/hades';

export default function LogsLoading() {
  return (
    <Card column>
      <CardContent flush padded>
        <Stack gap="snug">
          {/* Toolbar skeleton */}
          <Stack direction="horizontal" align="center" justify="between" gap="comfortable">
            <Skeleton shape="block" width="half" size="md" />
            <Stack direction="horizontal" align="center" gap="comfortable">
              <Skeleton shape="line" width="quarter" size="xs" />
              <Skeleton shape="block" width="quarter" size="md" />
            </Stack>
          </Stack>

          {/* Row skeletons */}
          {Array.from({ length: 8 }).map((_, i) => (
            <Stack key={i} direction="horizontal" align="center" gap="comfortable">
              <Skeleton shape="line" width="three-quarters" size="sm" />
              <Skeleton shape="line" width="half" size="sm" />
              <Skeleton shape="line" width="three-quarters" size="sm" />
              <Skeleton shape="block" width="quarter" size="xs" />
              <Skeleton shape="line" width="half" size="sm" />
              <Skeleton shape="line" width="half" size="sm" />
              <Skeleton shape="line" width="quarter" size="sm" />
            </Stack>
          ))}
        </Stack>
      </CardContent>
    </Card>
  );
}
