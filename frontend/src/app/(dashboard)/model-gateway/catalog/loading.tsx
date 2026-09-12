// loading.tsx — Suspense fallback for the model catalog page.
//
// Mirrors the CatalogTab layout (page header, stat card grid, tab strip,
// model table) so there is no layout shift when data arrives.

import {
  Card,
  CardContent,
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

function StatCardSkeleton() {
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

export default function CatalogLoading() {
  return (
    <Stack gap="comfortable">
      {/* Page header */}
      <Stack gap="snug">
        <Skeleton shape="line" width="quarter" size="lg" />
        <Skeleton shape="line" width="half" size="xs" />
      </Stack>

      {/* Stat cards */}
      <Grid cols={{ base: 2, lg: 4 }} gap="snug">
        {Array.from({ length: 4 }).map((_, i) => (
          <StatCardSkeleton key={i} />
        ))}
      </Grid>

      {/* Tab strip: Virtual Models / Provider Catalog */}
      <Stack direction="horizontal" gap="snug">
        <Skeleton shape="block" width="third" size="md" />
      </Stack>

      {/* Model table */}
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
                  <TableHead srOnly>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {Array.from({ length: 8 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell>
                      <Stack gap="none">
                        <Skeleton shape="line" width="three-quarters" size="sm" />
                        <Skeleton shape="line" width="full" size="xs" />
                      </Stack>
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
                      <Skeleton shape="circle" size="xs" />
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
