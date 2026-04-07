import * as React from 'react';
import { cn } from '../../utils';
import { Heading, Text } from '../typography/typography';

/**
 * Card
 *
 * A surface container that lifts content one step above the page background.
 * Cards use `bg-surface` and `border-border-subtle` to express depth through
 * background lightness progression (ADR SD-3 — no shadows).
 *
 * Compose with `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`,
 * and `CardFooter` for consistent internal spacing.
 *
 * Pass `column` when the card needs to be a flex column — required for
 * `CardFooter`'s `mt-auto` to push the footer to the card's bottom edge.
 *
 * ```tsx
 * <Card>
 *   <CardHeader>
 *     <CardTitle>Agent: apollo-3</CardTitle>
 *     <CardDescription>Last active 2 minutes ago</CardDescription>
 *   </CardHeader>
 *   <CardContent>...</CardContent>
 *   <CardFooter>...</CardFooter>
 * </Card>
 * ```
 *
 * Full-height card with a sticky footer:
 *
 * ```tsx
 * <Card column>
 *   <CardHeader>...</CardHeader>
 *   <CardContent grow>...</CardContent>
 *   <CardFooter>...</CardFooter>
 * </Card>
 * ```
 */

// ─── Card ─────────────────────────────────────────────────────────────────────

interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * When true, makes the card a flex column container. Required when you need
   * `CardFooter` to stick to the bottom via `mt-auto`, or when `CardContent`
   * should grow to fill remaining height with `grow`.
   *
   * ```tsx
   * <Card column>
   *   <CardContent grow>…</CardContent>
   *   <CardFooter>…</CardFooter>
   * </Card>
   * ```
   */
  column?: boolean;
}

const Card = React.forwardRef<HTMLDivElement, CardProps>(
  ({ className, column = false, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        'rounded-lg border border-border-subtle bg-surface text-foreground',
        column && 'flex flex-col',
        className,
      )}
      {...props}
    />
  ),
);
Card.displayName = 'Card';

// ─── CardHeader ───────────────────────────────────────────────────────────────

interface CardHeaderProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * When true, reduces the bottom padding from `pb-6` to `pb-2`. Use in
   * stat cards and compact contexts where the header sits immediately above
   * a metric value and the default gap feels too wide.
   *
   * ```tsx
   * <CardHeader compact>
   *   <CardTitle>Costs</CardTitle>
   * </CardHeader>
   * ```
   */
  compact?: boolean;

  /**
   * Optional icon anchored to the top-right of the header. Renders in a
   * small elevated box inset from the card corner. Use a single Lucide icon
   * (size-4) to represent the card's subject at a glance.
   *
   * ```tsx
   * <CardHeader compact icon={<DollarSign className="size-4" />}>
   *   <CardTitle>Costs</CardTitle>
   *   <CardDescription>Current billing period</CardDescription>
   * </CardHeader>
   * ```
   */
  icon?: React.ReactNode;
}

/**
 * CardHeader
 *
 * Top section of a card. Provides consistent horizontal padding and vertical
 * rhythm between a title/description group and the card body.
 *
 * Pass `compact` to reduce bottom padding for stat cards and KPI widgets
 * where the standard gap between header and content feels excessive.
 *
 * Pass `icon` to anchor a small icon box to the top-right corner of the
 * header — useful for KPI cards and stat widgets.
 */
const CardHeader = React.forwardRef<HTMLDivElement, CardHeaderProps>(
  ({ className, compact = false, icon, children, ...props }, ref) => (
    <div
      ref={ref}
      className={cn('flex flex-col gap-1.5 p-6', compact && 'pb-2', className)}
      {...props}
    >
      {icon ? (
        <div className="flex items-start justify-between gap-4">
          <div className="flex flex-col gap-1.5" style={{ marginTop: '-6px' }}>
            {children}
          </div>
          <span className="-mt-3 -mr-3 flex size-8 shrink-0 items-center justify-center rounded-md bg-surface-elevated border border-border-subtle text-muted-foreground">
            {icon}
          </span>
        </div>
      ) : (
        children
      )}
    </div>
  ),
);
CardHeader.displayName = 'CardHeader';

// ─── CardTitle ────────────────────────────────────────────────────────────────

/**
 * CardTitle
 *
 * Primary heading inside a `CardHeader`. Delegates directly to the Terra
 * `Heading` primitive (variant="label" renders an `<h4>` by default).
 */
const CardTitle = React.forwardRef<
  HTMLHeadingElement,
  Omit<React.HTMLAttributes<HTMLHeadingElement>, 'color'>
>(({ className, ...props }, ref) => (
  <Heading ref={ref} variant="label" className={cn('leading-tight', className)} {...props} />
));
CardTitle.displayName = 'CardTitle';

// ─── CardDescription ──────────────────────────────────────────────────────────

/**
 * CardDescription
 *
 * Supporting text beneath `CardTitle`. Delegates to the Terra `Text`
 * primitive so colour and size stay in the token system.
 */
const CardDescription = React.forwardRef<
  HTMLParagraphElement,
  Omit<React.HTMLAttributes<HTMLParagraphElement>, 'color'>
>(({ className, ...props }, ref) => (
  <Text
    ref={ref}
    size="supporting"
    color="muted"
    className={cn('leading-snug', className)}
    {...props}
  />
));
CardDescription.displayName = 'CardDescription';

// ─── CardContent ──────────────────────────────────────────────────────────────

interface CardContentProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * When `true`, adds top padding equal to the card's standard spacing grid
   * (`pt-6`). Use this when `CardContent` is the first (or only) child of a
   * `Card` — i.e. when there is no `CardHeader` above it to supply the
   * leading rhythm.
   *
   * Without a header the default `pt-0` leaves the content flush against the
   * card edge. Rather than reaching for `className="pt-6"`, pass `padded`:
   *
   * Card with a header — no `padded` needed (header provides top spacing):
   *
   * ```tsx
   * <Card>
   *   <CardHeader><CardTitle>Status</CardTitle></CardHeader>
   *   <CardContent>…</CardContent>
   * </Card>
   * ```
   *
   * Card without a header — use `padded` to restore top spacing:
   *
   * ```tsx
   * <Card>
   *   <CardContent padded>…</CardContent>
   * </Card>
   * ```
   */
  padded?: boolean;

  /**
   * When `true`, removes all padding so content reaches the card edges.
   * Use for edge-to-edge tables, image fills, and list-based card bodies
   * where the content provides its own internal spacing.
   *
   * Takes precedence over `padded` when both are set.
   *
   * ```tsx
   * <Card>
   *   <CardHeader>…</CardHeader>
   *   <CardContent flush>
   *     <TableRoot>…</TableRoot>
   *   </CardContent>
   * </Card>
   * ```
   */
  flush?: boolean;

  /**
   * When `true`, the content region grows to fill remaining vertical space
   * in a flex-column `Card`. Pair with `<Card column>` and a `CardFooter`
   * to push the footer to the card's bottom edge.
   *
   * ```tsx
   * <Card column>
   *   <CardContent grow>…</CardContent>
   *   <CardFooter>…</CardFooter>
   * </Card>
   * ```
   */
  grow?: boolean;
}

/**
 * CardContent
 *
 * Main body region of the card. Top padding is `pt-0` by default because
 * `CardHeader` already provides the leading rhythm. When the card has no
 * header, pass `padded` to restore the top spacing. Pass `flush` for
 * edge-to-edge content (tables, full-bleed images). Pass `grow` inside a
 * `<Card column>` to fill remaining height.
 */
const CardContent = React.forwardRef<HTMLDivElement, CardContentProps>(
  ({ className, padded = false, flush = false, grow = false, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        flush ? 'p-0' : cn('px-6 pb-6', padded ? 'pt-6' : 'pt-0'),
        grow && 'flex-1',
        className,
      )}
      {...props}
    />
  ),
);
CardContent.displayName = 'CardContent';

// ─── CardFooter ───────────────────────────────────────────────────────────────

interface CardFooterProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * Controls horizontal alignment of footer content.
   *
   *   start   — left-aligned (default)
   *   center  — centred, e.g. for a single "Load more" action
   *   between — space-between, e.g. for a count label + action button pair
   *
   * ```tsx
   * <CardFooter justify="between">
   *   <Text size="caption" color="subtle">Showing 6 of 12</Text>
   *   <Button variant="ghost" size="sm">Load more</Button>
   * </CardFooter>
   * ```
   *
   * ```tsx
   * <CardFooter justify="center">
   *   <Button variant="ghost" size="sm">Open event log</Button>
   * </CardFooter>
   * ```
   */
  justify?: 'start' | 'center' | 'between';

  /**
   * When `true`, reduces vertical padding from `pb-6 pt-0` to `py-3`.
   * Use for compact footers in dense card layouts where the standard gap
   * creates too much visual weight below the content.
   *
   * ```tsx
   * <CardFooter compact justify="between">
   *   <Text size="caption" color="subtle">Updated 30 s ago</Text>
   *   <Button variant="ghost" size="sm">Details</Button>
   * </CardFooter>
   * ```
   */
  compact?: boolean;
}

const FOOTER_JUSTIFY: Record<NonNullable<CardFooterProps['justify']>, string> = {
  start: 'justify-start',
  center: 'justify-center',
  between: 'justify-between',
};

/**
 * CardFooter
 *
 * Bottom action or metadata strip of a card. Applies a top border and uses
 * flex to align actions horizontally.
 *
 * Use `justify` to control horizontal alignment and `compact` to tighten
 * vertical padding in dense layouts.
 *
 * Note: `mt-auto` is always present — it only takes effect when the parent
 * `Card` has `column` (i.e. `flex flex-col`) applied.
 */
const CardFooter = React.forwardRef<HTMLDivElement, CardFooterProps>(
  ({ className, justify = 'start', compact = false, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        'flex flex-wrap items-center gap-3 px-6 border-t border-border-subtle mt-auto',
        compact ? 'py-3' : 'pb-6 pt-0',
        FOOTER_JUSTIFY[justify],
        className,
      )}
      {...props}
    />
  ),
);
CardFooter.displayName = 'CardFooter';

// ─── Exports ──────────────────────────────────────────────────────────────────

export { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter };
export type { CardProps, CardHeaderProps, CardContentProps, CardFooterProps };
