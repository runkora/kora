// loading.tsx — Suspense fallback for the personal usage page.
//
// Mirrors the MyUsageShell layout (dashboard header, KPI strip, usage chart,
// PATs table) so there is no layout shift when data arrives.

import {
  Card,
  CardContent,
  CardHeader,
  Grid,
  Skeleton,
  Stack,
  TableRoot,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@orbit/hades';

function KPICardSkeleton() {
  return (
    <Card inset border="subtle">
      <CardContent padded compact>
        <Stack gap="tight">
          <Skeleton shape="line" width="half" size="xs" />
          <Skeleton shape="line" width="quarter" size="lg" />
        </Stack>
      </CardContent>
    </Card>
  );
}

export default function MyUsageLoading() {
  return (
    <Stack gap="comfortable">
      {/* Dashboard header + period controls */}
      <Stack direction="horizontal" align="start" justify="between">
        <Stack gap="tight">
          <Skeleton shape="line" width="quarter" size="lg" />
          <Skeleton shape="line" width="half" size="xs" />
        </Stack>
        <Skeleton shape="block" width="quarter" size="sm" />
      </Stack>

      {/* KPI strip */}
      <Grid cols={{ base: 2, lg: 4 }} gap="snug">
        {Array.from({ length: 4 }).map((_, i) => (
          <KPICardSkeleton key={i} />
        ))}
      </Grid>

      {/* Usage chart */}
      <Card column>
        <CardHeader compact>
          <Skeleton shape="line" width="third" size="xs" />
        </CardHeader>
        <CardContent>
          <Skeleton shape="block" width="full" size="xl" />
        </CardContent>
      </Card>

      {/* PATs table */}
      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  {Array.from({ length: 4 }).map((_, i) => (
                    <TableHead key={i}>
                      <Skeleton shape="line" width="half" size="xs" />
                    </TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {Array.from({ length: 4 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell>
                      <Skeleton shape="line" width="three-quarters" size="sm" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="line" width="half" size="sm" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="line" width="half" size="sm" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="line" width="quarter" size="sm" />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableRoot>
        </CardContent>
      </Card>
    </Stack>
  );
}
