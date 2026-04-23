// loading.tsx — Suspense fallback for the models tab.
//
// Mirrors DataTable's exact rendered structure (TableContainer > toolbar skeleton
// > TableRoot > table) so there is no layout shift when data arrives.
// 7 visible columns: favourite · name · provider · status · capabilities · dialects · actions

import {
  Badge,
  Skeleton,
  Stack,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableHeader,
  TableRoot,
  Table,
  TableRow,
} from '@orbit/terra';

export default function ModelsLoading() {
  return (
    <TableContainer>
      {/* Toolbar skeleton: search left, count + toggle right */}
      <Stack asChild direction="horizontal" align="center" justify="between" gap="comfortable">
        <section className="px-4 py-3 border-b border-border">
          <Skeleton shape="block" width="quarter" size="sm" />
          <Stack direction="horizontal" align="center" gap="comfortable">
            <Skeleton shape="line" width="quarter" size="xs" />
            <Skeleton shape="block" width="quarter" size="sm" />
          </Stack>
        </section>
      </Stack>

      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead />
              <TableHead>
                <Skeleton shape="line" width="half" size="xs" />
              </TableHead>
              <TableHead>
                <Skeleton shape="line" width="half" size="xs" />
              </TableHead>
              <TableHead>
                <Skeleton shape="line" width="half" size="xs" />
              </TableHead>
              <TableHead>
                <Skeleton shape="line" width="full" size="xs" />
              </TableHead>
              <TableHead>
                <Skeleton shape="line" width="full" size="xs" />
              </TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {Array.from({ length: 12 }).map((_, i) => (
              <TableRow key={i}>
                <TableCell>
                  <Skeleton shape="circle" size="xs" />
                </TableCell>
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
                  <Skeleton shape="circle" size="xs" />
                </TableCell>
                <TableCell>
                  <Stack direction="horizontal" gap="tight">
                    <Badge variant="outline" size="sm">
                      <Skeleton shape="line" width="full" size="xs" />
                    </Badge>
                    <Badge variant="outline" size="sm">
                      <Skeleton shape="line" width="full" size="xs" />
                    </Badge>
                  </Stack>
                </TableCell>
                <TableCell>
                  <Stack direction="horizontal" gap="tight">
                    <Badge variant="outline" size="sm">
                      <Skeleton shape="line" width="full" size="xs" />
                    </Badge>
                  </Stack>
                </TableCell>
                <TableCell>
                  <Skeleton shape="circle" size="xs" />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableRoot>
    </TableContainer>
  );
}
