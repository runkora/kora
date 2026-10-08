// loading.tsx — Suspense fallback for the budgets page.
//
// Mirrors the BudgetsShell layout (tab strip, tenant cap card, per-principal
// budgets table) so there is no layout shift when data arrives.

import {
  Card,
  CardContent,
  CardHeader,
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

export default function BudgetsLoading() {
  return (
    <Stack gap="comfortable">
      {/* Tab strip: Principals / Role Budgets / Grant Budgets */}
      <Stack direction="horizontal" gap="snug">
        <Skeleton shape="block" width="third" size="md" />
      </Stack>

      {/* Tenant cap card */}
      <Card column>
        <CardHeader compact>
          <Skeleton shape="line" width="quarter" size="xs" />
        </CardHeader>
        <CardContent>
          <Stack gap="snug">
            <Stack direction="horizontal" align="center" justify="between">
              <Skeleton shape="line" width="quarter" size="sm" />
              <Skeleton shape="line" width="quarter" size="sm" />
            </Stack>
            <Skeleton shape="block" width="full" size="xs" />
          </Stack>
        </CardContent>
      </Card>

      {/* Per-principal budgets table */}
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
                      <Skeleton shape="block" width="full" size="xs" />
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
