// Suspense fallback for the logs tab.
//
// Mirrors the DataTable layout (TableContainer > toolbar > table) so there is
// no layout shift when data arrives. Also serves as the Suspense boundary
// required by the client-side useSearchParams() call inside LogsTab.

import {
  Skeleton,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableHeader,
  TableRoot,
  TableRow,
} from '@orbit/terra';

function ToolbarSkeleton() {
  return (
    <Stack asChild direction="horizontal" align="center" justify="between" gap="comfortable">
      <section className="px-4 py-3 border-b border-border-subtle">
        {/* Search input */}
        <Skeleton shape="block" width="half" size="md" />
        {/* Right side: count + columns toggle */}
        <Stack direction="horizontal" align="center" gap="comfortable">
          <Skeleton shape="line" width="quarter" size="xs" />
          <Skeleton shape="block" width="quarter" size="md" />
        </Stack>
      </section>
    </Stack>
  );
}

function TableRowSkeleton() {
  return (
    <TableRow>
      <TableCell>
        <Skeleton shape="line" width="three-quarters" size="sm" />
      </TableCell>
      <TableCell>
        <Skeleton shape="line" width="half" size="sm" />
      </TableCell>
      <TableCell>
        <Skeleton shape="line" width="three-quarters" size="sm" />
      </TableCell>
      <TableCell>
        <Skeleton shape="block" width="quarter" size="xs" />
      </TableCell>
      <TableCell>
        <Skeleton shape="line" width="half" size="sm" />
      </TableCell>
      <TableCell>
        <Skeleton shape="block" width="quarter" size="xs" />
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
  );
}

export default function LogsLoading() {
  return (
    <TableContainer>
      <ToolbarSkeleton />
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Request ID</TableHead>
              <TableHead>Date</TableHead>
              <TableHead>Model</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Key</TableHead>
              <TableHead>Key status</TableHead>
              <TableHead>Latency</TableHead>
              <TableHead>Tokens in / out</TableHead>
              <TableHead>Cost</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {Array.from({ length: 8 }).map((_, i) => (
              <TableRowSkeleton key={i} />
            ))}
          </TableBody>
        </Table>
      </TableRoot>
    </TableContainer>
  );
}
