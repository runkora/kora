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
      <div className="flex items-center gap-3 px-4 py-3 border-b border-border">
        <Skeleton shape="block" width="quarter" size="sm" />
        <div className="ml-auto flex items-center gap-4">
          <Skeleton shape="line" width="quarter" size="xs" />
          <Skeleton shape="block" width="quarter" size="sm" />
        </div>
      </div>

      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>
                <div className="w-10" />
              </TableHead>
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
              <TableHead>
                <div className="w-10" />
              </TableHead>
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
