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
} from '@runkora/hades';

export default function RoutesLoading() {
  return (
    <Stack gap="comfortable">
      <Skeleton shape="line" width="quarter" size="lg" />
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
                      <Skeleton shape="line" width="half" size="sm" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="line" width="three-quarters" size="sm" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="line" width="quarter" size="sm" />
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
    </Stack>
  );
}
