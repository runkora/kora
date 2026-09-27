/** @tier 3 — marketplace-scoped. Skeleton of the overview toolbar, Skills table, and pagination bar. */

import {
  Card,
  CardContent,
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

/**
 * Stands in for the toolbar and the Skill results while no Marketplace data
 * exists: in loading.tsx during the first server load, and in the overview
 * during a retry after a failed first load.
 */
export function MarketplaceResultsSkeleton() {
  return (
    <>
      {/* Toolbar: search input, Marketplace picker, sort select, view toggle. */}
      <Stack direction="horizontal" align="center" gap="snug">
        <Stack grow>
          <Skeleton shape="block" width="full" size="md" />
        </Stack>
        <Skeleton shape="block" width="quarter" size="md" />
        <Skeleton shape="block" width="quarter" size="md" />
        <Stack direction="horizontal" gap="tight">
          <Skeleton shape="circle" size="md" />
          <Skeleton shape="circle" size="md" />
        </Stack>
      </Stack>

      <Stack gap="comfortable">
        {/* Skills table: Skill (name and slug), Description, Marketplace with chevron. */}
        <Card column>
          <CardContent flush>
            <TableRoot>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>
                      <Skeleton shape="line" width="half" size="xs" />
                    </TableHead>
                    <TableHead>
                      <Skeleton shape="line" width="half" size="xs" />
                    </TableHead>
                    <TableHead>
                      <Skeleton shape="line" width="half" size="xs" />
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {Array.from({ length: 8 }).map((_, i) => (
                    <TableRow key={i}>
                      <TableCell>
                        <Stack gap="tight">
                          <Skeleton shape="line" width="three-quarters" size="sm" />
                          <Skeleton shape="line" width="half" size="xs" />
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <Skeleton shape="line" width="full" size="sm" />
                      </TableCell>
                      <TableCell>
                        <Skeleton shape="line" width="three-quarters" size="sm" />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableRoot>
          </CardContent>
        </Card>

        {/* Pagination bar: result range + rows select left, page controls right. */}
        <Stack direction="horizontal" align="center" justify="between" gap="comfortable">
          <Skeleton shape="line" width="quarter" size="sm" />
          <Skeleton shape="block" width="third" size="md" />
        </Stack>
      </Stack>
    </>
  );
}
