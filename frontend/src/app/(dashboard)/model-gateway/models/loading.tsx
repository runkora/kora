// loading.tsx — Suspense fallback for the Models page.
//
// Next.js renders this instantly on client-side navigation while the RSC
// page.tsx fetches model data server-side. Mirrors the real page structure
// (header → favourites → separator → all models table) so there is no
// jarring layout shift when the real content streams in.

import {
  Stack,
  Grid,
  Section,
  Skeleton,
  Separator,
  Heading,
  Text,
  Card,
  CardContent,
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
    <Section>
      <Stack gap="spacious">
        {/* ── Page header ── */}
        <Stack gap="snug">
          <Heading variant="title">Models</Heading>
          <Text size="supporting" color="muted">
            Browse available AI models and manage your favourites.
          </Text>
        </Stack>

        {/* ── Favourites section ── */}
        <Stack gap="snug">
          <Heading variant="section">Favourites</Heading>
          <Grid cols={3} gap="snug">
            <Card>
              <CardContent>
                <Skeleton shape="block" width="full" size="xl" />
              </CardContent>
            </Card>
            <Card>
              <CardContent>
                <Skeleton shape="block" width="full" size="xl" />
              </CardContent>
            </Card>
            <Card>
              <CardContent>
                <Skeleton shape="block" width="full" size="xl" />
              </CardContent>
            </Card>
          </Grid>
        </Stack>

        <Separator />

        {/* ── All models section ── */}
        <Stack gap="comfortable">
          <Stack gap="snug">
            <Heading variant="section">All models</Heading>
            <Text size="supporting" color="muted">
              Click any row to view full details. Use the column toggle to customise your view.
            </Text>
          </Stack>

          {/* Toolbar — real search input so there's no pop-in when data loads */}
          <Stack direction="horizontal" justify="between" align="center" gap="snug">
            <Skeleton shape="line" width="quarter" size="xs" />
            <input
              type="search"
              placeholder="Search…"
              disabled
              className="h-8 w-72 rounded-md border border-border bg-surface-elevated px-3 text-xs text-foreground placeholder:text-muted-foreground"
            />
          </Stack>

          {/* Table */}
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>
                    <Skeleton shape="line" width="full" size="xs" />
                  </TableHead>
                  <TableHead>
                    <Skeleton shape="line" width="full" size="xs" />
                  </TableHead>
                  <TableHead>
                    <Skeleton shape="line" width="full" size="xs" />
                  </TableHead>
                  <TableHead>
                    <Skeleton shape="line" width="full" size="xs" />
                  </TableHead>
                  <TableHead>
                    <Skeleton shape="line" width="full" size="xs" />
                  </TableHead>
                  <TableHead>
                    <Skeleton shape="line" width="full" size="xs" />
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {Array.from({ length: 10 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell>
                      <Skeleton shape="circle" size="xs" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="circle" size="xs" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="line" width="half" size="sm" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="line" width="three-quarters" size="sm" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="line" width="two-thirds" size="sm" />
                    </TableCell>
                    <TableCell>
                      <Skeleton shape="line" width="half" size="sm" />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableRoot>
        </Stack>
      </Stack>
    </Section>
  );
}
