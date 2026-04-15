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

export default function RolesLoading() {
  return (
    <Stack gap="comfortable">
      <Stack direction="horizontal" align="center" justify="between">
        <Skeleton shape="line" width="quarter" size="lg" />
        <Skeleton shape="block" width="quarter" size="sm" />
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
                    <span className="w-10 block" />
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {Array.from({ length: 6 }).map((_, i) => (
                  <TableRow key={i}>
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
