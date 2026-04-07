// loading.tsx — Suspense fallback for the models tab.
//
// Mirrors the real models tab layout exactly (toolbar + card-wrapped table)
// so there is no layout shift when data arrives.

import {
  Badge,
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

export default function ModelsLoading() {
  return (
    <Stack gap="comfortable">
      {/* Toolbar — real search input so it looks seamless */}
      <Stack direction="horizontal" align="center" gap="snug" wrap>
        <span className="relative flex-1 max-w-xs">
          <input
            type="text"
            placeholder="Search models..."
            disabled
            className="w-full rounded-md border border-border bg-surface px-3 py-1.5 pl-8 text-sm text-foreground placeholder-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          />
        </span>
        <span className="flex-1" />
        <Skeleton shape="line" width="quarter" size="xs" />
        <Skeleton shape="block" width="quarter" size="sm" />
      </Stack>

      {/* Table inside a card — mirrors the Card column + CardContent flush structure */}
      <Card column>
        <CardContent flush>
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  {/* favourite */}
                  <TableHead>
                    <span className="w-10 block" />
                  </TableHead>
                  {/* name */}
                  <TableHead>
                    <Skeleton shape="line" width="half" size="xs" />
                  </TableHead>
                  {/* provider */}
                  <TableHead>
                    <Skeleton shape="line" width="half" size="xs" />
                  </TableHead>
                  {/* status */}
                  <TableHead>
                    <Skeleton shape="line" width="half" size="xs" />
                  </TableHead>
                  {/* capabilities */}
                  <TableHead>
                    <Skeleton shape="line" width="full" size="xs" />
                  </TableHead>
                  {/* dialects */}
                  <TableHead>
                    <Skeleton shape="line" width="full" size="xs" />
                  </TableHead>
                  {/* actions */}
                  <TableHead>
                    <span className="w-10 block" />
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
        </CardContent>
      </Card>
    </Stack>
  );
}
