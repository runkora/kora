// Suspense fallback for the models tab. Mirrors the real DataTable's grid
// columns and row heights so real data doesn't reflow the page when it lands.

import { Card, CardContent, Grid, Skeleton, Stack } from '@clear-route/hades';

const GRID_TEMPLATE_COLUMNS = '2.5rem 1fr 1fr 1fr 1fr 1fr 2.5rem';
const ROW_STYLE = {
  gridTemplateColumns: GRID_TEMPLATE_COLUMNS,
  columnGap: '14px',
  paddingInline: '1rem',
  // Matches the real row's `items-center` — Grid itself defaults to stretch.
  alignItems: 'center' as const,
};
const HEADER_BORDER = { borderBottom: '1px solid var(--color-border-subtle)' };

function HeaderRowSkeleton() {
  return (
    <Grid style={{ ...ROW_STYLE, ...HEADER_BORDER, paddingBlock: '9px' }}>
      <Stack />
      <Skeleton shape="line" width="quarter" size="xs" /> {/* Model */}
      <Skeleton shape="line" width="third" size="xs" /> {/* Availability */}
      <Skeleton shape="line" width="third" size="xs" /> {/* Capabilities */}
      <Skeleton shape="line" width="half" size="xs" /> {/* Effective Price */}
      <Skeleton shape="line" width="quarter" size="xs" /> {/* Dialects */}
      <Stack />
    </Grid>
  );
}

function ChipsCellSkeleton() {
  return (
    <Stack direction="horizontal" wrap gap="tight">
      <Skeleton shape="block" width="third" size="sm" />
      <Skeleton shape="block" width="third" size="sm" />
    </Stack>
  );
}

type LineSpec = { width: 'half' | 'three-quarters'; size: 'sm' | 'xs' };

// Two-line cell (model id, effective price) — sizes/widths tuned to match
// each real line's height and typical length.
function TwoLineCellSkeleton({ first, second }: { first: LineSpec; second: LineSpec }) {
  return (
    <Stack gap="tight">
      <Skeleton shape="line" width={first.width} size={first.size} />
      <Skeleton shape="line" width={second.width} size={second.size} />
    </Stack>
  );
}

function BodyRowSkeleton({ isLast }: { isLast: boolean }) {
  return (
    <Grid
      style={{
        ...ROW_STYLE,
        ...(isLast ? {} : HEADER_BORDER),
        paddingBlock: '12px',
      }}
    >
      <Skeleton shape="circle" size="xs" />
      <TwoLineCellSkeleton
        first={{ width: 'half', size: 'xs' }}
        second={{ width: 'three-quarters', size: 'sm' }}
      />
      <Stack direction="horizontal" align="center" gap="tight">
        <Skeleton shape="circle" size="xs" />
        <Skeleton shape="line" width="half" size="sm" />
      </Stack>
      <ChipsCellSkeleton />
      <TwoLineCellSkeleton
        first={{ width: 'half', size: 'sm' }}
        second={{ width: 'three-quarters', size: 'xs' }}
      />
      <ChipsCellSkeleton />
      <Skeleton shape="circle" size="xs" />
    </Grid>
  );
}

const ROW_COUNT = 10;

export default function ModelsLoading() {
  return (
    <Card column>
      <CardContent flush>
        <Stack gap="none">
          {/* Toolbar — its own box, matching the real toolbar's border-b + min-h-[52px]. */}
          <Stack
            direction="horizontal"
            align="center"
            justify="between"
            gap="comfortable"
            style={{
              paddingInline: '1rem',
              paddingBlock: '0.75rem',
              minHeight: '52px',
              ...HEADER_BORDER,
            }}
          >
            <Skeleton shape="block" width="quarter" size="sm" />
            <Stack direction="horizontal" align="center" gap="comfortable">
              <Skeleton shape="line" width="quarter" size="xs" />
              <Skeleton shape="block" width="quarter" size="sm" />
            </Stack>
          </Stack>

          <HeaderRowSkeleton />

          {Array.from({ length: ROW_COUNT }).map((_, i) => (
            <BodyRowSkeleton key={i} isLast={i === ROW_COUNT - 1} />
          ))}
        </Stack>
      </CardContent>
    </Card>
  );
}
