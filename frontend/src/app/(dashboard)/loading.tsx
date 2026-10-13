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
} from '@clear-route/hades';

/**
 * The dashboard home fetches agents, runs, connections, environments and
 * inference analytics in one server pass, so the skeleton stands in for the
 * whole surface: the header, the inventory KPI strip, the agent types table,
 * and the activity/spend card pair beneath it.
 */
export default function DashboardLoading() {
  return (
    <Stack gap="relaxed">
      <Stack gap="tight">
        <Skeleton shape="line" width="quarter" size="lg" />
        <Skeleton shape="line" width="half" size="xs" />
      </Stack>

      {/* The inventory KPI strip. */}
      <Grid cols={{ base: 2, lg: 4 }} gap="comfortable">
        {Array.from({ length: 4 }).map((_, i) => (
          <Card key={i} inset border="subtle">
            <CardContent padded compact>
              <Stack gap="tight">
                <Skeleton shape="line" width="half" size="xs" />
                <Skeleton shape="line" width="quarter" size="lg" />
              </Stack>
            </CardContent>
          </Card>
        ))}
      </Grid>

      {/* The agent types table. */}
      <Card column>
        <CardHeader>
          <Stack direction="horizontal" align="center" justify="between">
            <Stack gap="tight">
              <Skeleton shape="line" width="quarter" size="sm" />
              <Skeleton shape="line" width="half" size="xs" />
            </Stack>
            <Skeleton shape="block" width="quarter" size="sm" />
          </Stack>
        </CardHeader>
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
                  <TableHead>
                    <Skeleton shape="line" width="half" size="xs" />
                  </TableHead>
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
                      <Skeleton shape="line" width="full" size="sm" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="line" width="half" size="sm" />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableRoot>
        </CardContent>
      </Card>

      {/* Recent runs and spend vs budget. */}
      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
        {Array.from({ length: 2 }).map((_, i) => (
          <Card key={i} column>
            <CardHeader>
              <Stack gap="tight">
                <Skeleton shape="line" width="quarter" size="sm" />
                <Skeleton shape="line" width="half" size="xs" />
              </Stack>
            </CardHeader>
            <CardContent>
              <Stack gap="snug">
                <Skeleton shape="line" width="full" size="sm" />
                <Skeleton shape="line" width="three-quarters" size="sm" />
                <Skeleton shape="line" width="full" size="sm" />
                <Skeleton shape="line" width="two-thirds" size="sm" />
              </Stack>
            </CardContent>
          </Card>
        ))}
      </Grid>
    </Stack>
  );
}
