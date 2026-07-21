import type { Meta, StoryObj } from '@storybook/react';
import { Badge } from '../badge/badge';
import { Button } from '../button/button';
import { Card, CardContent, CardHeader } from '../card/card';
import { Heading, Text } from '../typography/typography';
import { Stack } from './stack';

const meta: Meta<typeof Stack> = {
  title: 'Terra/Layout/Stack',
  component: Stack,
  parameters: {
    layout: 'padded',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
  argTypes: {
    direction: {
      control: 'select',
      options: ['vertical', 'horizontal'],
      description: 'Flex direction. Pass a Responsive value for breakpoint-specific overrides.',
    },
    gap: {
      control: 'select',
      options: ['none', 'tight', 'snug', 'comfortable', 'relaxed', 'loose', 'spacious'],
      description: 'Space between children (token-aligned). Accepts a Responsive value.',
    },
    align: {
      control: 'select',
      options: [undefined, 'start', 'center', 'end', 'stretch', 'baseline'],
      description: 'Cross-axis alignment (items-*).',
    },
    justify: {
      control: 'select',
      options: [undefined, 'start', 'center', 'end', 'between', 'around', 'evenly'],
      description: 'Main-axis distribution (justify-*).',
    },
    wrap: {
      control: 'boolean',
      description: 'Allow children to wrap onto multiple lines.',
    },
  },
};

export default meta;
type Story = StoryObj<typeof Stack>;

// ── Placeholder box ────────────────────────────────────────────────────────────

const Box = ({ label }: { label?: string }) => (
  <div className="h-12 rounded-md border border-border-subtle bg-surface flex items-center justify-center px-3">
    {label && (
      <Text size="caption" color="subtle">
        {label}
      </Text>
    )}
  </div>
);

// ── Shared nav links — reused in asChild comparison ────────────────────────────

const NAV_ITEMS = ['Overview', 'Agents', 'Policies', 'Events'];

const NavLinks = () => (
  <>
    {NAV_ITEMS.map((item) => (
      <a
        key={item}
        href="#"
        className="block px-3 py-2 rounded-md text-sm text-muted hover:text-foreground hover:bg-surface-elevated transition-colors"
      >
        {item}
      </a>
    ))}
  </>
);

// ── Stories ────────────────────────────────────────────────────────────────────

/** Default vertical stack — the most common layout primitive. */
export const Vertical: Story = {
  render: () => (
    <Stack gap="comfortable" className="w-80">
      <Box label="Item 1" />
      <Box label="Item 2" />
      <Box label="Item 3" />
    </Stack>
  ),
};

/** Horizontal row with centred cross-axis alignment. */
export const Horizontal: Story = {
  render: () => (
    <Stack direction="horizontal" align="center" gap="comfortable">
      <Box label="A" />
      <Box label="B" />
      <Box label="C" />
    </Stack>
  ),
};

/**
 * Space-between row — the canonical heading + action toolbar pattern.
 * Replace the wrapping `<div className="flex items-center justify-between">`.
 */
export const ToolbarRow: Story = {
  name: 'Justify: between (toolbar)',
  render: () => (
    <Stack direction="horizontal" justify="between" align="center" className="w-full max-w-xl">
      <Heading variant="label">Active agents</Heading>
      <Button size="sm" variant="secondary">
        New agent
      </Button>
    </Stack>
  ),
};

/** Gap scale — all seven token values visualised side by side. */
export const GapScale: Story = {
  name: 'Gap scale',
  render: () => (
    <Stack gap="loose" className="w-full max-w-xl">
      {(['none', 'tight', 'snug', 'comfortable', 'relaxed', 'loose', 'spacious'] as const).map(
        (g) => (
          <Stack key={g} direction="horizontal" align="center" gap="comfortable">
            <Text size="caption" color="subtle" className="w-16 shrink-0 font-mono">
              {g}
            </Text>
            <Stack direction="horizontal" gap={g} className="flex-1">
              <Box label="·" />
              <Box label="·" />
              <Box label="·" />
            </Stack>
          </Stack>
        ),
      )}
    </Stack>
  ),
};

/**
 * Wrapping badge cluster — `wrap` lets children reflow onto new lines
 * instead of overflowing at narrow widths.
 */
export const WrappingCluster: Story = {
  name: 'Wrap: badge cluster',
  render: () => (
    <Stack direction="horizontal" wrap gap="tight" className="max-w-64">
      {['policy-engine', 'event-spine', 'auth', 'credentials', 'agents', 'tools', 'identity'].map(
        (tag) => (
          <Badge key={tag} variant="secondary">
            {tag}
          </Badge>
        ),
      )}
    </Stack>
  ),
};

/**
 * Responsive direction — vertical on mobile, horizontal on medium+.
 * Resize the preview viewport to see the layout switch.
 */
export const ResponsiveDirection: Story = {
  name: 'Responsive direction',
  render: () => (
    <Stack direction={{ base: 'vertical', md: 'horizontal' }} gap={{ base: 'snug', md: 'relaxed' }}>
      {[
        { label: 'Total runs', value: '4,821' },
        { label: 'Active agents', value: '14' },
        { label: 'Policy violations', value: '0' },
      ].map(({ label, value }) => (
        <Card key={label} className="flex-1">
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
    </Stack>
  ),
};

/**
 * `asChild` comparison — both panels render identical navigation links.
 * The difference is structural, not visual. Open DevTools and inspect the DOM:
 *
 * **Without `asChild`** — Stack wraps `<nav>` in a `<div>`:
 * ```
 * <div class="flex flex-col gap-1 …">   ← Stack's div
 *   <nav>                                ← your element
 *     <a>…</a>
 *   </nav>
 * </div>
 * ```
 *
 * **With `asChild`** — Stack merges its classes onto `<nav>` directly:
 * ```
 * <nav class="flex flex-col gap-1 …">   ← Stack + nav fused, no wrapper
 *   <a>…</a>
 * </nav>
 * ```
 *
 * Prefer `asChild` whenever the child element should semantically own the
 * layout — `<nav>`, `<ul>`, `<form>`, `<main>`, `<section>`, etc.
 */
export const AsChildComparison: Story = {
  name: 'asChild: with vs without',
  render: () => (
    <Stack direction="horizontal" gap="loose" align="start">
      {/* ── Without asChild ────────────────────────────────── */}
      <Stack gap="snug" className="flex-1">
        <Text size="caption" color="subtle" className="font-mono">
          without asChild
        </Text>
        <Text size="caption" color="muted">
          Stack renders a wrapper {'<div>'} around {'<nav>'}. Inspect the DOM to see two elements.
        </Text>
        {/*
         * DOM output:
         *   <div class="flex flex-col gap-1 …">  ← Stack's own div
         *     <nav>                               ← your semantic element inside
         *       <a>…</a>
         *     </nav>
         *   </div>
         */}
        <Stack gap="tight" className="w-48 rounded-md border border-border-subtle p-2">
          <nav>
            <NavLinks />
          </nav>
        </Stack>
      </Stack>

      {/* ── With asChild ───────────────────────────────────── */}
      <Stack gap="snug" className="flex-1">
        <Text size="caption" color="subtle" className="font-mono">
          with asChild
        </Text>
        <Text size="caption" color="muted">
          Stack merges its classes onto {'<nav>'} via Slot. No wrapper div in the DOM.
        </Text>
        {/*
         * DOM output:
         *   <nav class="flex flex-col gap-1 …">  ← nav IS the flex container
         *     <a>…</a>
         *   </nav>
         */}
        <Stack asChild gap="tight" className="w-48 rounded-md border border-border-subtle p-2">
          <nav>
            <NavLinks />
          </nav>
        </Stack>
      </Stack>
    </Stack>
  ),
};

/**
 * Semantic nav via `asChild` — standalone example.
 * Stack's flex + gap classes are merged directly onto `<nav>` with no wrapper div.
 */
export const AsChildNav: Story = {
  name: 'asChild: semantic nav',
  render: () => (
    <Stack asChild gap="tight" className="w-48">
      <nav>
        <NavLinks />
      </nav>
    </Stack>
  ),
};

/**
 * Nested stacks — complex card layouts composed entirely from Stack
 * without a single layout-specific className.
 */
export const NestedStacks: Story = {
  name: 'Nested stacks (credential card)',
  render: () => (
    <Stack gap="comfortable" className="w-96 rounded-lg border border-border-subtle bg-surface p-6">
      <Stack direction="horizontal" justify="between" align="center">
        <Heading variant="label">Credential broker</Heading>
        <Badge variant="healthy" dot>
          Healthy
        </Badge>
      </Stack>
      <Stack gap="tight">
        {[
          { label: 'Provider', value: 'AWS us-east-1' },
          { label: 'Expires', value: '12 min' },
          { label: 'Scope', value: 'S3, DynamoDB (read)' },
        ].map(({ label, value }) => (
          <Stack key={label} direction="horizontal" justify="between" align="center">
            <Text size="supporting" color="muted">
              {label}
            </Text>
            <Text size="supporting">{value}</Text>
          </Stack>
        ))}
      </Stack>
      <Stack direction="horizontal" justify="end" gap="snug">
        <Button variant="ghost" size="sm">
          Revoke
        </Button>
        <Button variant="secondary" size="sm">
          Renew
        </Button>
      </Stack>
    </Stack>
  ),
};
