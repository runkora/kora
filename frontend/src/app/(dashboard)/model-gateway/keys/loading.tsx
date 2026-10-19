// loading.tsx — Suspense fallback for the keys tab.
//
// Mirrors the real keys tab layout (header + card-per-key) so there is no
// layout shift when data arrives.

import {
  Button,
  Card,
  CardContent,
  CardHeader,
  Grid,
  Heading,
  Skeleton,
  Stack,
  Text,
} from '@runkora/hades';

function KeyCardSkeleton() {
  return (
    <Card>
      <CardHeader compact>
        <Stack direction="horizontal" align="center" justify="between">
          <Stack direction="horizontal" align="center" gap="snug">
            <Skeleton shape="line" width="third" size="sm" />
            <Skeleton shape="block" width="quarter" size="xs" />
          </Stack>
          <Skeleton shape="circle" size="xs" />
        </Stack>
      </CardHeader>
      <CardContent>
        <Grid cols={{ base: 2, md: 4 }} gap="snug">
          {Array.from({ length: 4 }).map((_, i) => (
            <Stack key={i} gap="none">
              <Skeleton shape="line" width="half" size="xs" />
              <Skeleton shape="line" width="three-quarters" size="sm" />
            </Stack>
          ))}
        </Grid>
      </CardContent>
    </Card>
  );
}

export default function KeysLoading() {
  return (
    <Stack gap="comfortable">
      {/* Header */}
      <Stack direction="horizontal" align="start" justify="between">
        <Stack gap="none">
          <Heading variant="label">API keys</Heading>
          <Text size="supporting" color="muted">
            Manage your API keys and monitor their usage
          </Text>
        </Stack>
        <Button variant="default" size="sm" disabled>
          Create key
        </Button>
      </Stack>

      {/* Cards */}
      {Array.from({ length: 3 }).map((_, i) => (
        <KeyCardSkeleton key={i} />
      ))}
    </Stack>
  );
}
