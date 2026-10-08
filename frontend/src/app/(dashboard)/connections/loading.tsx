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
} from '@orbit/hades';

/**
 * The Connections page fetches the read-only connection catalog and per-connection
 * agent usage in one server pass, so the skeleton stands in for the whole surface:
 * the header and the connections table beneath it.
 */
export default function ConnectionsLoading() {
  return (
    <Stack gap="comfortable">
      {/* Page header: title + read-only explainer. */}
      <Stack gap="tight">
        <Skeleton shape="line" width="quarter" size="lg" />
        <Skeleton shape="line" width="half" size="xs" />
      </Stack>

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
