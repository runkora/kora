// loading.tsx — Suspense fallback for the Model Gateway overview page.
//
// Mirrors the OverviewTab layout (page header, KPI card grid, chart cards,
// recent activity table) so there is no layout shift when data arrives.

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
} from '@orbit/terra';

function KPICardSkeleton() {
  return (
    <Card column>
      <CardHeader compact>
        <Skeleton shape="line" width="half" size="xs" />
      </CardHeader>
      <CardContent>
        <Stack gap="snug">
          <Skeleton shape="line" width="third" size="md" />
          <Skeleton shape="line" width="half" size="xs" />
        </Stack>
      </CardContent>
    </Card>
  );
}

function ChartCardSkeleton() {
  return (
    <Card column>
      <CardHeader compact>
        <Skeleton shape="line" width="third" size="xs" />
      </CardHeader>
      <CardContent>
        <Skeleton shape="block" width="full" size="xl" />
      </CardContent>
    </Card>
  );
}

export default function OverviewLoading() {
  return (
    <Stack gap="comfortable">
      {/* Page header */}
      <Stack gap="snug">
        <Skeleton shape="line" width="quarter" size="lg" />
        <Skeleton shape="line" width="half" size="xs" />
      </Stack>

      {/* KPI cards */}
      <Grid cols={{ base: 1, sm: 2, lg: 3 }} gap="comfortable">
        {Array.from({ length: 3 }).map((_, i) => (
          <KPICardSkeleton key={i} />
        ))}
      </Grid>

      {/* Charts */}
      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
        <ChartCardSkeleton />
        <ChartCardSkeleton />
      </Grid>

      {/* Recent activity table */}
      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  {Array.from({ length: 5 }).map((_, i) => (
                    <TableHead key={i}>
                      <Skeleton shape="line" width="half" size="xs" />
                    </TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {Array.from({ length: 6 }).map((_, i) => (
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
