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

/**
 * The Triggers page loads sources, triggers, deliveries and agent types in one
 * server pass, so the skeleton stands in for the whole surface: the header, the
 * KPI strip, and the trigger table beneath it.
 */
export default function TriggersLoading() {
  return (
    <Stack gap="comfortable">
      <Stack direction="horizontal" align="center" justify="between">
        <Stack gap="tight">
          <Skeleton shape="line" width="quarter" size="lg" />
          <Skeleton shape="line" width="half" size="xs" />
        </Stack>
        <Skeleton shape="block" width="quarter" size="sm" />
      </Stack>

      {/* The fleet KPI strip. */}
      <Grid cols={4} gap="snug">
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
                  <TableHead>
                    <Skeleton shape="line" width="half" size="xs" />
                  </TableHead>
                  <TableHead srOnly>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {Array.from({ length: 8 }).map((_, i) => (
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
