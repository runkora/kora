import * as React from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cn } from '../../utils';
import { GAP, resolveResponsive } from './spacing';
import { MAX_W, MIN_W, MAX_H, MIN_H, TEXT_ALIGN } from './sizing';
import type { Responsive, SpacingSize } from './spacing';
import type { ContainerWidth, ContainerHeight, TextAlign } from './sizing';

// ── Types ─────────────────────────────────────────────────────────────────────

export type StackDirection = 'vertical' | 'horizontal';
export type StackAlign = 'start' | 'center' | 'end' | 'stretch' | 'baseline';
export type StackJustify = 'start' | 'center' | 'end' | 'between' | 'around' | 'evenly';

// ── Lookup tables ─────────────────────────────────────────────────────────────
// Full string literals required — Tailwind v4's static scanner cannot handle
// template literals or dynamic class construction.

const DIRECTION: Record<'base' | 'sm' | 'md' | 'lg' | 'xl', Record<StackDirection, string>> = {
  base: { vertical: 'flex-col',    horizontal: 'flex-row' },
  sm:   { vertical: 'sm:flex-col', horizontal: 'sm:flex-row' },
  md:   { vertical: 'md:flex-col', horizontal: 'md:flex-row' },
  lg:   { vertical: 'lg:flex-col', horizontal: 'lg:flex-row' },
  xl:   { vertical: 'xl:flex-col', horizontal: 'xl:flex-row' },
};

const ALIGN_CLASSES: Record<StackAlign, string> = {
  start:    'items-start',
  center:   'items-center',
  end:      'items-end',
  stretch:  'items-stretch',
  baseline: 'items-baseline',
};

const JUSTIFY_CLASSES: Record<StackJustify, string> = {
  start:   'justify-start',
  center:  'justify-center',
  end:     'justify-end',
  between: 'justify-between',
  around:  'justify-around',
  evenly:  'justify-evenly',
};

// ── Props ─────────────────────────────────────────────────────────────────────

export interface StackProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * Flex direction — `vertical` (column) or `horizontal` (row).
   * Accepts a `Responsive` value so direction can flip at breakpoints.
   *
   * Default: `'vertical'`
   *
   * ```tsx
   * // Always a column
   * <Stack>…</Stack>
   *
   * // Column on mobile, row on medium+
   * <Stack direction={{ base: 'vertical', md: 'horizontal' }}>…</Stack>
   * ```
   */
  direction?: Responsive<StackDirection>;

  /**
   * Space between children. Accepts a `SpacingSize` token or a
   * `Responsive` value for breakpoint-specific gaps.
   *
   * Default: `'snug'`
   *
   * ```tsx
   * <Stack gap="comfortable">…</Stack>
   * <Stack gap={{ base: 'snug', lg: 'relaxed' }}>…</Stack>
   * ```
   */
  gap?: Responsive<SpacingSize>;

  /**
   * Cross-axis alignment (`items-*`). Applied at every breakpoint.
   *
   * ```tsx
   * <Stack direction="horizontal" align="center">…</Stack>
   * ```
   */
  align?: StackAlign;

  /**
   * Main-axis distribution (`justify-*`). Applied at every breakpoint.
   *
   * ```tsx
   * <Stack direction="horizontal" justify="between">
   *   <Text>Left</Text>
   *   <Button>Action</Button>
   * </Stack>
   * ```
   */
  justify?: StackJustify;

  /**
   * When `true`, allows children to wrap onto multiple lines (`flex-wrap`).
   * Useful for tag clusters, button groups, and badge rows that should reflow
   * instead of overflow at narrow widths.
   *
   * ```tsx
   * <Stack direction="horizontal" wrap gap="tight">
   *   {tags.map(t => <Badge key={t}>{t}</Badge>)}
   * </Stack>
   * ```
   */
  wrap?: boolean;

  /**
   * Maximum width constraint. Accepts a semantic `ContainerWidth` token
   * or a `Responsive` value for breakpoint-specific widths.
   *
   * ```tsx
   * // Constrain subtitle to a readable measure
   * <Stack maxWidth="prose">
   *   <Text size="lead" color="muted">Control plane for autonomous AI agents.</Text>
   * </Stack>
   *
   * // AI chat canvas at comfortable document width
   * <Stack maxWidth="document">…</Stack>
   *
   * // Narrow on mobile, full-width on large screens
   * <Stack maxWidth={{ base: 'card', lg: 'full' }}>…</Stack>
   * ```
   */
  maxWidth?: Responsive<ContainerWidth>;

  /**
   * Minimum width constraint. Accepts a semantic `ContainerWidth` token
   * or a `Responsive` value.
   *
   * ```tsx
   * <Stack minWidth="compact">…</Stack>
   * ```
   */
  minWidth?: Responsive<ContainerWidth>;

  /**
   * Minimum height constraint. Accepts a semantic `ContainerHeight` token.
   *
   * `'screen'` uses `min-h-dvh` — dynamic viewport height, correct on mobile
   * browsers where the address bar resizes the visible area.
   *
   * ```tsx
   * // Full-screen page wrapper
   * <Stack asChild minHeight="screen" direction="vertical" align="center" justify="center">
   *   <main>…</main>
   * </Stack>
   * ```
   */
  minHeight?: Responsive<ContainerHeight>;

  /**
   * Maximum height constraint. Accepts a semantic `ContainerHeight` token.
   *
   * ```tsx
   * <Stack maxHeight="screen" style={{ overflowY: 'auto' }}>…</Stack>
   * ```
   */
  maxHeight?: Responsive<ContainerHeight>;

  /**
   * Text alignment for all descendant text nodes. Emits a single `text-*`
   * class — does not cascade differently at breakpoints (pass `className` for
   * that edge case). Omitting the prop inherits alignment from the parent.
   *
   * ```tsx
   * <Stack textAlign="center">
   *   <Heading variant="display">Orbit</Heading>
   *   <Text size="lead" color="muted">…</Text>
   * </Stack>
   * ```
   */
  textAlign?: TextAlign;

  /**
   * When `true`, merges Stack's layout classes directly onto the immediate child
   * element via Radix Slot instead of rendering a wrapper `<div>`. Use this to
   * apply flex layout to semantic HTML elements without adding an extra DOM node.
   *
   * **Without `asChild`** — Stack renders a `<div>` that wraps your element:
   *
   * ```tsx
   * // DOM: <div class="flex flex-col gap-2"><nav>…</nav></div>
   * <Stack gap="snug">
   *   <nav>…</nav>
   * </Stack>
   * ```
   *
   * **With `asChild`** — layout classes land on `<nav>` directly, no extra div:
   *
   * ```tsx
   * // DOM: <nav class="flex flex-col gap-2">…</nav>
   * <Stack asChild gap="snug">
   *   <nav>…</nav>
   * </Stack>
   * ```
   *
   * Works with any single-child element: `<nav>`, `<ul>`, `<form>`, `<main>`, etc.
   * The child must accept a `className` prop (all HTML elements and most components do).
   */
  asChild?: boolean;
}

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * Stack
 *
 * A flex container that replaces ad-hoc `<div className="flex flex-col gap-* …">`.
 * Stack is the primary tool for laying out sequences of elements — vertically
 * (default) or horizontally — with consistent, token-aligned spacing.
 *
 * Direction, gap, and sizing props are all `Responsive`, so layouts adapt
 * naturally to viewport width without reaching for `className`.
 *
 * For equal-column grid layouts use `Grid` instead.
 *
 * ```tsx
 * // Vertical list (default) with comfortable gap
 * <Stack gap="comfortable">
 *   <Card>…</Card>
 *   <Card>…</Card>
 * </Stack>
 *
 * // Horizontal toolbar: space between, cross-axis centred
 * <Stack direction="horizontal" justify="between" align="center">
 *   <Heading variant="label">Active agents</Heading>
 *   <Button size="sm">New agent</Button>
 * </Stack>
 *
 * // Centered full-screen page wrapper (no extra DOM node)
 * // DOM: <main class="flex flex-col min-h-dvh items-center justify-center">…</main>
 * <Stack asChild direction="vertical" align="center" justify="center" minHeight="screen">
 *   <main>…</main>
 * </Stack>
 *
 * // Without asChild — Stack renders a <div> that wraps the child element
 * // DOM: <div class="flex flex-col gap-2"><nav>…</nav></div>
 * <Stack gap="snug">
 *   <nav>…</nav>
 * </Stack>
 *
 * // With asChild — layout classes merge onto the child, no wrapper div in DOM
 * // DOM: <nav class="flex flex-col gap-2">…</nav>
 * <Stack asChild gap="snug">
 *   <nav>…</nav>
 * </Stack>
 * ```
 */
const Stack = React.forwardRef<HTMLDivElement, StackProps>(
  (
    {
      className,
      direction = 'vertical',
      gap = 'snug',
      align,
      justify,
      wrap = false,
      maxWidth,
      minWidth,
      minHeight,
      maxHeight,
      textAlign,
      asChild = false,
      ...props
    },
    ref,
  ) => {
    const Comp = (asChild ? Slot : 'div') as React.ElementType;
    return (
      <Comp
        ref={ref}
        className={cn(
          'flex',
          resolveResponsive(direction, DIRECTION, 'vertical'),
          resolveResponsive(gap, GAP, 'snug'),
          align && ALIGN_CLASSES[align],
          justify && JUSTIFY_CLASSES[justify],
          wrap && 'flex-wrap',
          maxWidth  && resolveResponsive(maxWidth,  MAX_W, 'full'),
          minWidth  && resolveResponsive(minWidth,  MIN_W, 'compact'),
          minHeight && resolveResponsive(minHeight, MIN_H, 'full'),
          maxHeight && resolveResponsive(maxHeight, MAX_H, 'full'),
          textAlign && TEXT_ALIGN[textAlign],
          className,
        )}
        {...props}
      />
    );
  },
);

Stack.displayName = 'Stack';

export { Stack };
