import { Card, CardContent, CardHeader, CardTitle, Skeleton, Stack } from '@runkora/hades';

export default function ProfileLoading() {
  return (
    <Stack gap="comfortable">
      <Stack gap="none">
        <Skeleton shape="line" width="quarter" size="md" />
        <Skeleton shape="line" width="third" size="xs" />
      </Stack>
      <Card column>
        <CardHeader compact>
          <CardTitle>
            <Skeleton shape="line" width="quarter" size="xs" />
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Stack gap="snug">
            <Skeleton shape="line" width="half" size="sm" />
            <Skeleton shape="line" width="three-quarters" size="xs" />
            <Skeleton shape="line" width="two-thirds" size="xs" />
            <Skeleton shape="line" width="half" size="xs" />
          </Stack>
        </CardContent>
      </Card>
      <Card column>
        <CardHeader compact>
          <CardTitle>
            <Skeleton shape="line" width="quarter" size="xs" />
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Stack gap="snug">
            <Skeleton shape="line" width="half" size="sm" />
            <Skeleton shape="line" width="full" size="xs" />
            <Skeleton shape="line" width="three-quarters" size="xs" />
            <Skeleton shape="line" width="full" size="xs" />
          </Stack>
        </CardContent>
      </Card>
      <Card column>
        <CardHeader compact>
          <CardTitle>
            <Skeleton shape="line" width="quarter" size="xs" />
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Stack gap="snug">
            <Skeleton shape="line" width="third" size="sm" />
            <Skeleton shape="line" width="full" size="xs" />
            <Skeleton shape="line" width="full" size="xs" />
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
