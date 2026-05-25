import * as React from 'react';
import { cn } from '../../utils';
import { Heading, Text } from '../typography/typography';
import { Icon } from '../icon/icon';
import type { IconName } from '../icon/registry';
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
}

const Card = React.forwardRef<HTMLDivElement, CardProps>(
  ({ className, column = false, interactive = false, overflowY, maxHeight, ...props }, ref) => (
    <div
      ref={ref}
      data-interactive={interactive ? 'true' : undefined}
      className={cn(
        'rounded-lg border border-border-subtle bg-surface text-foreground overflow-hidden',
        column && 'flex flex-col',
        interactive &&
          'cursor-pointer transition-colors hover:bg-surface-elevated/50 focus-within:border-accent focus-within:ring-1 focus-within:ring-accent/30',
        overflowY && OVERFLOW_Y[overflowY],
        maxHeight && MAX_H.base[maxHeight],
        className,
      )}
      {...props}
    />
  ),
);
Card.displayName = 'Card';

// ─── CardHeader ───────────────────────────────────────────────────────────────

export interface CardHeaderProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * When true, reduces vertical padding for stat cards and KPI widgets.
   *
   * ```tsx
   * <CardHeader compact>
   *   <CardTitle>Costs</CardTitle>
   * </CardHeader>
   * ```
   */
  compact?: boolean;

  /**
   * Optional icon anchored to the top-right corner of the header. Renders in
   * a small elevated box — useful for KPI cards and stat widgets.
   *
   * For section-style cards with a left-leading icon use `leadingIcon` instead.
   *
   * ```tsx
   * <CardHeader compact icon={<Icon name="dollar-sign" size="sm" />}>
   *   <CardTitle>Costs</CardTitle>
   * </CardHeader>
   * ```
   */
  icon?: React.ReactNode;

  /**
   * Left-leading icon rendered inline before the title. Use for section cards
   * and content cards where the icon contextualises the subject.
   *
   * ```tsx
   * <CardHeader leadingIcon="user" title="Identity" description="Agent identity settings" />
   * ```
   */
  leadingIcon?: IconName;

  /**
   * Shorthand title — renders a `CardTitle` inside the header. Can be combined
   * with `leadingIcon`, `description`, and `action`.
   */
  title?: string;

  /**
   * Shorthand subtitle — renders a `CardDescription` (muted, supporting size)
   * below the title. Pass a string for default styling or a ReactNode for
   * custom formatting (e.g. a mono slug).
   */
  description?: React.ReactNode;

  /**
   * Right-side slot for a button, badge, or other action. Pinned to the
   * trailing edge of the header row.
   */
  action?: React.ReactNode;

  /**
   * When true, renders a `border-b border-border-subtle` divider between the
   * header and the card body. Use for section cards with rich content below.
   *
   * ```tsx
   * <CardHeader leadingIcon="settings" title="Behaviour" divider />
   * ```
   */
  divider?: boolean;
}

/**
 * CardHeader
 *
 * Top section of a card. Two usage modes:
 *
 * **Section card anatomy** — pass `leadingIcon`, `title`, `description`, and
 * optionally `action`. The header composes its own internal layout and renders
 * a divider when `divider` is set.
 *
 * ```tsx
 * <CardHeader
 *   leadingIcon="user"
 *   title="Identity"
 *   description="Configure agent identity and display name"
 *   action={<Button size="sm" variant="ghost">Edit</Button>}
 *   divider
 * />
 * ```
 *
 * **Legacy children mode** — pass `CardTitle` / `CardDescription` as children.
 * Optionally pass `icon` for the KPI corner icon and `compact` to tighten padding.
 *
 * ```tsx
 * <CardHeader compact icon={<Icon name="activity" size="sm" />}>
 *   <CardTitle>Total Requests</CardTitle>
 *   <CardDescription>Last billing period</CardDescription>
 * </CardHeader>
 * ```
 */
const CardHeader = React.forwardRef<HTMLDivElement, CardHeaderProps>(
  (
    {
      className,
      compact = false,
      icon,
      leadingIcon,
      title,
      description,
      action,
      divider = false,
      children,
      ...props
    },
    ref,
  ) => {
    const hasNewAnatomy =
      leadingIcon !== undefined ||
      title !== undefined ||
      description !== undefined ||
      action !== undefined;

    return (
      <div
        ref={ref}
        className={cn(
          'flex flex-col gap-1.5 px-4',
          compact ? 'py-2.5' : 'py-3.5',
          divider && 'border-b border-border-subtle',
          className,
        )}
        {...props}
      >
        {hasNewAnatomy ? (
          <>
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-2.5 min-w-0">
                {leadingIcon && (
                  <span className="shrink-0 text-muted-foreground">
                    <Icon name={leadingIcon} size="sm" />
                  </span>
                )}
                <div className="flex flex-col gap-0.5 min-w-0">
                  {title !== undefined && <CardTitle>{title}</CardTitle>}
                  {description !== undefined &&
                    (typeof description === 'string' ? (
                      <CardDescription>{description}</CardDescription>
                    ) : (
                      description
                    ))}
                </div>
              </div>
              {action !== undefined && <div className="shrink-0">{action}</div>}
            </div>
            {children}
          </>
        ) : icon ? (
          <div className="flex items-center justify-between gap-4">
            <div className="flex flex-col gap-1.5 min-w-0">{children}</div>
            <div className="shrink-0 flex size-9 items-center justify-center rounded-lg bg-surface-elevated border border-border-subtle text-muted-foreground">
              {icon}
            </div>
          </div>
        ) : (
          children
        )}
      </div>
    );
  },
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
        flush ? 'p-0' : cn('px-4 pb-4', padded ? 'pt-4' : 'pt-0'),
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
        'flex flex-wrap items-center gap-3 px-4 border-t border-border-subtle mt-auto',
        compact ? 'py-3' : 'py-4',
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
export type { CardProps, CardContentProps, CardFooterProps };
