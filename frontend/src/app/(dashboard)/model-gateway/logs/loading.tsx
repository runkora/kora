// loading.tsx — Suspense fallback for the logs tab.
//
// Mirrors the real logs tab layout (toolbar + table) so there is no layout
// shift when data arrives. Also serves as the Suspense boundary required by
// the client-side useSearchParams() call inside LogsTab.

import {
  Button,
  Card,
  CardContent,
  Skeleton,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRoot,
  TableRow,
} from '@orbit/terra';

function ToolbarSkeleton() {
  return (
    <Stack direction="horizontal" align="center" justify="between" gap="comfortable" wrap>
      {/* Search input */}
      <Skeleton shape="block" width="half" size="md" />
      {/* Key filter dropdown */}
      <Skeleton shape="block" width="quarter" size="md" />
      {/* Status filter buttons */}
      <Stack direction="horizontal" gap="snug">
        {Array.from({ length: 3 }).map((_, i) => (
          <Button key={i} size="xs" variant="ghost" disabled>
            <Skeleton shape="line" width="full" size="xs" />
          </Button>
        ))}
      </Stack>
    </Stack>
  );
}

function TableRowSkeleton() {
  return (
    <TableRow>
      {/* Request ID */}
      <TableCell>
        <Skeleton shape="line" width="three-quarters" size="sm" />
      </TableCell>
      {/* Time */}
      <TableCell>
        <Skeleton shape="line" width="half" size="sm" />
      </TableCell>
      {/* Model */}
      <TableCell>
        <Skeleton shape="line" width="three-quarters" size="sm" />
      </TableCell>
      {/* Status */}
      <TableCell>
        <Skeleton shape="block" width="quarter" size="xs" />
      </TableCell>
      {/* Key */}
      <TableCell>
        <Stack gap="none">
          <Skeleton shape="line" width="half" size="sm" />
          <Skeleton shape="line" width="three-quarters" size="xs" />
        </Stack>
      </TableCell>
      {/* Key status */}
      <TableCell>
        <Skeleton shape="block" width="quarter" size="xs" />
      </TableCell>
      {/* Latency */}
      <TableCell>
        <Skeleton shape="line" width="half" size="sm" />
      </TableCell>
      {/* Tokens in / out */}
      <TableCell>
        <Skeleton shape="line" width="half" size="sm" />
      </TableCell>
      {/* Cost */}
      <TableCell>
        <Skeleton shape="line" width="quarter" size="sm" />
      </TableCell>
    </TableRow>
  );
}

export default function LogsLoading() {
  return (
    <Stack gap="comfortable">
      <ToolbarSkeleton />

      {/* Summary line */}
      <Skeleton shape="line" width="quarter" size="xs" />

      {/* Table */}
      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Request ID</TableHead>
                  <TableHead>Time</TableHead>
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
        </CardContent>
      </Card>
    </Stack>
  );
}
