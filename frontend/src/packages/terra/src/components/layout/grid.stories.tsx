import type { Meta, StoryObj } from '@storybook/react';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '../card/card';
import { Heading, Text } from '../typography/typography';
import { Grid } from './grid';
import { Stack } from './stack';

const meta: Meta<typeof Grid> = {
  title: 'Terra/Layout/Grid',
  component: Grid,
  parameters: {
    layout: 'padded',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
  argTypes: {
    cols: {
      control: 'select',
      options: [1, 2, 3, 4, 5, 6, 12],
      description: 'Number of equal-width columns. Accepts a Responsive value.',
    },
    gap: {
      control: 'select',
      options: ['none', 'tight', 'snug', 'comfortable', 'relaxed', 'loose', 'spacious'],
      description: 'Uniform gap between cells. Overridden per-axis by gapX / gapY.',
    },
    gapX: {
      control: 'select',
      options: [undefined, 'none', 'tight', 'snug', 'comfortable', 'relaxed', 'loose', 'spacious'],
      description: 'Horizontal (column) gap — overrides gap on the x-axis.',
    },
    gapY: {
      control: 'select',
      options: [undefined, 'none', 'tight', 'snug', 'comfortable', 'relaxed', 'loose', 'spacious'],
      description: 'Vertical (row) gap — overrides gap on the y-axis.',
    },
  },
};

export default meta;
type Story = StoryObj<typeof Grid>;

// ── Placeholder box ────────────────────────────────────────────────────────────

const Box = ({ label, tall = false }: { label?: string; tall?: boolean }) => (
  <div
    className={[
      'rounded-md border border-border-subtle bg-surface flex items-center justify-center',
      tall ? 'h-24' : 'h-12',
    ].join(' ')}
  >
    {label && (
      <Text size="caption" color="subtle">
        {label}
      </Text>
    )}
  </div>
);

// ── Shared list items — reused in asChild comparison ───────────────────────────

const FEATURE_ITEMS = ['Identity layer', 'Policy engine', 'Credential broker', 'Event spine'];

const FeatureListItems = () => (
  <>
    {FEATURE_ITEMS.map((item) => (
      <li key={item}>
        <Stack
          direction="horizontal"
          align="center"
          gap="snug"
          className="p-3 rounded-md bg-surface"
        >
          <div className="h-2 w-2 rounded-full bg-status-healthy shrink-0" aria-hidden />
          <Text size="supporting">{item}</Text>
        </Stack>
      </li>
    ))}
  </>
);

// ── Stories ────────────────────────────────────────────────────────────────────

/** Fixed 3-column stat card row — the simplest Grid use case. */
export const ThreeColumns: Story = {
  name: '3-column stat row',
  render: () => (
    <Grid cols={3} gap="comfortable">
      {[
        { label: 'Total runs', value: '4,821' },
        { label: 'Active agents', value: '14' },
        { label: 'Policy violations', value: '0' },
      ].map(({ label, value }) => (
        <Card key={label}>
          <CardHeader compact>
            <Text size="supporting" color="muted">
              {label}
            </Text>
          </CardHeader>
          <CardContent padded>
            <Heading variant="metric">{value}</Heading>
          </CardContent>
        </Card>
      ))}
    </Grid>
  ),
};

/**
 * Responsive columns — 1 col on mobile, 2 on small, 3 on large.
 * Always design mobile-first: start at 1 column and widen up.
 * Resize the preview to watch the grid reflow.
 */
export const ResponsiveCols: Story = {
  name: 'Responsive columns (1 → 2 → 3)',
  render: () => (
    <Grid cols={{ base: 1, sm: 2, lg: 3 }} gap={{ base: 'snug', lg: 'comfortable' }}>
      {Array.from({ length: 6 }, (_, i) => (
        <Card key={i}>
          <CardHeader>
            <CardTitle>Agent {i + 1}</CardTitle>
            <CardDescription>apollo-retriever-{i + 1}</CardDescription>
          </CardHeader>
          <CardContent>
            <Text size="supporting" color="muted">
              Processing {(i + 1) * 2} tasks across {i % 3 + 1} tools.
            </Text>
          </CardContent>
        </Card>
      ))}
    </Grid>
  ),
};

/** Column count showcase — 1 through 6. */
export const ColumnCounts: Story = {
  name: 'Column count scale',
  render: () => (
    <Stack gap="relaxed">
      {([1, 2, 3, 4, 6] as const).map((n) => (
        <Stack key={n} gap="tight">
          <Text size="caption" color="subtle" className="font-mono">
            cols={n}
          </Text>
          <Grid cols={n} gap="snug">
            {Array.from({ length: n }, (_, i) => (
              <Box key={i} label={`${i + 1}`} />
            ))}
          </Grid>
        </Stack>
      ))}
    </Stack>
  ),
};

/**
 * Asymmetric gaps — `gapX` and `gapY` let you control column and row
 * spacing independently when a uniform gap doesn't fit.
 */
export const AsymmetricGap: Story = {
  name: 'Asymmetric gap (gapX / gapY)',
  render: () => (
    <Stack gap="loose">
      <Stack gap="tight">
        <Text size="caption" color="subtle">
          gapX=&quot;snug&quot; gapY=&quot;loose&quot; — tight columns, generous rows
        </Text>
        <Grid cols={4} gapX="snug" gapY="loose">
          {Array.from({ length: 8 }, (_, i) => (
            <Box key={i} label={`${i + 1}`} tall />
          ))}
        </Grid>
      </Stack>
      <Stack gap="tight">
        <Text size="caption" color="subtle">
          gapX=&quot;loose&quot; gapY=&quot;tight&quot; — wide columns, tight rows
        </Text>
        <Grid cols={4} gapX="loose" gapY="tight">
          {Array.from({ length: 8 }, (_, i) => (
            <Box key={i} label={`${i + 1}`} tall />
          ))}
        </Grid>
      </Stack>
    </Stack>
  ),
};

/**
 * `asChild` comparison — both panels render the same four feature items in a
 * 2-column layout. The difference is structural, not visual. Open DevTools:
 *
 * **Without `asChild`** — Grid wraps `<ul>` in a `<div>`:
 * ```
 * <div class="grid grid-cols-2 gap-2 …">   ← Grid's div
 *   <ul class="list-none …">               ← your element inside
 *     <li>…</li>
 *   </ul>
 * </div>
 * ```
 *
 * **With `asChild`** — Grid merges its classes onto `<ul>` directly:
 * ```
 * <ul class="grid grid-cols-2 gap-2 list-none …">  ← ul IS the grid, no wrapper
 *   <li>…</li>
 * </ul>
 * ```
 *
 * Prefer `asChild` for semantic list grids, section grids, and any case where
 * the child element should own the grid context rather than be nested inside one.
 */
export const AsChildComparison: Story = {
  name: 'asChild: with vs without',
  render: () => (
    <Stack gap="loose">
      {/* ── Without asChild ──────────────────────────────────────── */}
      <Stack gap="snug">
        <Text size="caption" color="subtle" className="font-mono">without asChild</Text>
        <Text size="caption" color="muted">
          Grid renders a wrapper {'<div>'} around {'<ul>'}. Inspect the DOM to see two elements.
        </Text>
        {/*
         * DOM output:
         *   <div class="grid grid-cols-2 gap-2">  ← Grid's own div
         *     <ul class="list-none p-0 m-0">      ← your semantic list inside
         *       <li>…</li>
         *     </ul>
         *   </div>
         */}
        <Grid cols={{ base: 1, md: 2 }} gap="snug">
          <ul className="list-none p-0 m-0">
            <FeatureListItems />
          </ul>
        </Grid>
      </Stack>

      {/* ── With asChild ─────────────────────────────────────────── */}
      <Stack gap="snug">
        <Text size="caption" color="subtle" className="font-mono">with asChild</Text>
        <Text size="caption" color="muted">
          Grid merges its classes onto {'<ul>'} via Slot. No wrapper div in the DOM.
        </Text>
        {/*
         * DOM output:
         *   <ul class="grid grid-cols-2 gap-2 list-none p-0 m-0">  ← ul IS the grid
         *     <li>…</li>
         *   </ul>
         */}
        <Grid asChild cols={{ base: 1, md: 2 }} gap="snug">
          <ul className="list-none p-0 m-0">
            <FeatureListItems />
          </ul>
        </Grid>
      </Stack>
    </Stack>
  ),
};

/**
 * Semantic list via `asChild` — standalone example.
 * Grid applies grid layout directly to `<ul>` with no wrapper div.
 */
export const AsChildList: Story = {
  name: 'asChild: semantic list',
  render: () => (
    <Grid asChild cols={{ base: 1, md: 2 }} gap="snug">
      <ul className="list-none p-0 m-0">
        <FeatureListItems />
      </ul>
    </Grid>
  ),
};

/**
 * 12-column sub-grid — use `cols={12}` with `col-span-*` children for
 * precise layout control in dashboard-style views.
 */
export const TwelveColumnSubgrid: Story = {
  name: '12-column precision layout',
  render: () => (
    <Grid cols={12} gap="snug">
      <div className="col-span-8">
        <Card className="h-full">
          <CardHeader>
            <CardTitle>Event log</CardTitle>
          </CardHeader>
          <CardContent>
            <Text size="supporting" color="muted">
              col-span-8 — primary content area
            </Text>
          </CardContent>
        </Card>
      </div>
      <div className="col-span-4">
        <Card className="h-full">
          <CardHeader>
            <CardTitle>Inspector</CardTitle>
          </CardHeader>
          <CardContent>
            <Text size="supporting" color="muted">
              col-span-4 — sidebar panel
            </Text>
          </CardContent>
        </Card>
      </div>
      <div className="col-span-4">
        <Box label="col-span-4" tall />
      </div>
      <div className="col-span-4">
        <Box label="col-span-4" tall />
      </div>
      <div className="col-span-4">
        <Box label="col-span-4" tall />
      </div>
    </Grid>
  ),
};
