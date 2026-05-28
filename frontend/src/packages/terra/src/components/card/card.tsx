import * as React from 'react';
import { cn } from '../../utils';
import { Icon } from '../icon';
import { Heading, Text } from '../typography/typography';
import { MAX_H, OVERFLOW_Y } from '../layout/sizing';
import type { MaxHeight, OverflowValue } from '../layout/sizing';

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
 * Pass `toggleable` to make `CardContent` collapsible. The card manages its
 * own open/closed state and renders a `CardCollapseBar` at the bottom
 * automatically. Use `defaultOpen` to set the initial state (default: false).
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
 *
 * Collapsible card — CardContent is hidden until the user expands it:
 *
 * ```tsx
 * <Card column toggleable>
 *   <CardHeader>
 *     <CardTitle>Setup</CardTitle>
 *     <CardDescription>Follow these steps to get started.</CardDescription>
 *   </CardHeader>
 *   <CardContent>...</CardContent>
 * </Card>
 * ```
 */

// ─── CardContext ──────────────────────────────────────────────────────────────

interface CardContextValue {
  open: boolean;
}

const CardContext = React.createContext<CardContextValue | null>(null);

// ─── Card ─────────────────────────────────────────────────────────────────────

export interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
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

  /**
   * When true, the card tracks focus events from any descendant input or
   * interactive element and applies a visible accent border ring when any
   * child has focus.
   *
   * Use for composite input areas where you want the entire card to
   * "light up" when the user tabs into a contained input — for example,
   * a chat input card that wraps a `<textarea>` and action buttons:
   *
   * ```tsx
   * <Card interactive>
   *   <textarea … />
   *   <Button>Send</Button>
   * </Card>
   * ```
   *
   * Implemented via `focus-within:border-accent` + a subtle ring. The
   * `data-interactive` attribute is set so tests and E2E selectors can
   * target interactive cards explicitly.
   */
  interactive?: boolean;

  /**
   * `overflow-y` CSS property. Use `'auto'` to make the card contents
   * vertically scrollable within a fixed height or `maxHeight` constraint.
   * Card already applies `overflow-hidden` — `overflowY` adds a scroll layer.
   */
  overflowY?: OverflowValue;

  /**
   * Maximum height constraint. Use with `overflowY="auto"` for scrollable
   * card containers (e.g. picker dropdowns, bounded user lists).
   */
  maxHeight?: MaxHeight;

  /**
   * When true, the card manages its own expand/collapse state and renders a
   * `CardCollapseBar` at the bottom. `CardContent` inside the card is hidden
   * when collapsed — all other children (`CardHeader`, `CardFooter`) remain
   * visible at all times.
   *
   * Implies `column`. Use `defaultOpen` to set the initial state.
   *
   * ```tsx
   * <Card column toggleable>
   *   <CardHeader><CardTitle>Setup</CardTitle></CardHeader>
   *   <CardContent>…</CardContent>
   * </Card>
   * ```
   */
  toggleable?: boolean;

  /**
   * Initial open state when `toggleable` is true. Defaults to `false`
   * (collapsed). Has no effect on non-toggleable cards.
   */
  defaultOpen?: boolean;
}

const Card = React.forwardRef<HTMLDivElement, CardProps>(
  (
    {
      className,
      column = false,
      interactive = false,
      overflowY,
      maxHeight,
      toggleable = false,
      defaultOpen = false,
      children,
      ...props
    },
    ref,
  ) => {
    const [open, setOpen] = React.useState(defaultOpen);

    const el = (
      <div
        ref={ref}
        data-interactive={interactive ? 'true' : undefined}
        className={cn(
          'rounded-lg border border-border-subtle bg-surface text-foreground overflow-hidden',
          (column || toggleable) && 'flex flex-col',
          interactive &&
            'cursor-pointer transition-colors hover:bg-surface-elevated/50 focus-within:border-accent focus-within:ring-1 focus-within:ring-accent/30',
          overflowY && OVERFLOW_Y[overflowY],
          maxHeight && MAX_H.base[maxHeight],
          className,
        )}
        {...props}
      >
        {children}
        {toggleable && <CardCollapseBar open={open} onClick={() => setOpen((o) => !o)} />}
      </div>
    );

    if (!toggleable) return el;

    return <CardContext.Provider value={{ open }}>{el}</CardContext.Provider>;
  },
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
          <div className="-mt-3 -mr-3 flex size-8 shrink-0 items-center justify-center rounded-md bg-surface-elevated border border-border-subtle text-muted-foreground">
            {icon}
          </div>
        </div>
      ) : (
        children
      )}
    </div>
  ),
);
CardHeader.displayName = 'CardHeader';

// ─── CardTitle ────────────────────────────────────────────────────────────────

export interface CardTitleProps {
  children?: React.ReactNode;
  /** @internal Used for internal Terra composition via cn(). */
  className?: string;
  id?: string;
  'aria-label'?: string;
}

/**
 * CardTitle
 *
 * Primary heading inside a `CardHeader`. Delegates directly to the Terra
 * `Heading` primitive (variant="label" renders an `<h4>` by default).
 */
const CardTitle = React.forwardRef<HTMLHeadingElement, CardTitleProps>(
  ({ className, ...props }, ref) => (
    <Heading ref={ref} variant="label" className={cn('leading-tight', className)} {...props} />
  ),
);
CardTitle.displayName = 'CardTitle';

// ─── CardDescription ──────────────────────────────────────────────────────────

export interface CardDescriptionProps {
  children?: React.ReactNode;
  /** @internal Used for internal Terra composition via cn(). */
  className?: string;
  id?: string;
  'aria-label'?: string;
}

/**
 * CardDescription
 *
 * Supporting text beneath `CardTitle`. Delegates to the Terra `Text`
 * primitive so colour and size stay in the token system.
 */
const CardDescription = React.forwardRef<HTMLParagraphElement, CardDescriptionProps>(
  ({ className, ...props }, ref) => (
    <Text
      ref={ref}
      size="supporting"
      color="muted"
      className={cn('leading-snug', className)}
      {...props}
    />
  ),
);
CardDescription.displayName = 'CardDescription';

// ─── CardContent ──────────────────────────────────────────────────────────────

export interface CardContentProps extends React.HTMLAttributes<HTMLDivElement> {
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
 *
 * When rendered inside a `<Card toggleable>`, `CardContent` is hidden
 * automatically while the card is collapsed.
 */
const CardContent = React.forwardRef<HTMLDivElement, CardContentProps>(
  ({ className, padded = false, flush = false, grow = false, ...props }, ref) => {
    const ctx = React.useContext(CardContext);
    if (ctx && !ctx.open) return null;
    return (
      <div
        ref={ref}
        className={cn(
          flush ? 'p-0' : cn('px-6 pb-6', padded ? 'pt-6' : 'pt-0'),
          grow && 'flex-1',
          className,
        )}
        {...props}
      />
    );
  },
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
   *   end     — right-aligned, e.g. for a single confirm/submit action
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
   *
   * ```tsx
   * <CardFooter justify="end">
   *   <Button variant="default" size="sm">Save changes</Button>
   * </CardFooter>
   * ```
   */
  justify?: 'start' | 'center' | 'between' | 'end';

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
  end: 'justify-end',
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

// ─── CardCollapseBar ──────────────────────────────────────────────────────────

interface CardCollapseBarProps {
  /** Whether the associated card content is currently expanded. */
  open: boolean;
  /** Callback fired when the bar is clicked or activated via keyboard. */
  onClick: () => void;
  /** Accessible label for the toggle. Defaults to "Expand section" / "Collapse section". */
  'aria-label'?: string;
}

/**
 * CardCollapseBar
 *
 * A full-width accent-coloured button strip placed as the last child of a
 * `<Card column>`. The card's `overflow-hidden` clips the bar to the card's
 * bottom rounded corners automatically.
 *
 * Rendered automatically by `<Card toggleable>`. Internal to Terra — not part
 * of the public API.
 */
const CardCollapseBar = React.forwardRef<HTMLButtonElement, CardCollapseBarProps>(
  ({ open, onClick, 'aria-label': ariaLabel }, ref) => (
    <button
      ref={ref}
      type="button"
      data-slot="card-collapse-bar"
      data-open={open ? 'true' : 'false'}
      aria-expanded={open}
      aria-label={ariaLabel ?? (open ? 'Collapse section' : 'Expand section')}
      onClick={onClick}
      className="flex w-full items-center justify-center py-2 bg-accent hover:brightness-110 active:brightness-90 transition-[filter] duration-150 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-accent-fg mt-auto"
    >
      <Icon name={open ? 'chevron-up' : 'chevron-down'} size="sm" color="default" />
    </button>
  ),
);
CardCollapseBar.displayName = 'CardCollapseBar';

// ─── Exports ──────────────────────────────────────────────────────────────────

export { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter };
export type { CardHeaderProps, CardFooterProps };
