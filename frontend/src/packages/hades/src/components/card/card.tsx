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
   * When true, renders the card recessed into its container: the background
   * step below the surface it sits on, with a subtle border.
   *
   * Use for read-only detail inside an already-elevated container — a panel's
   * field rows, a stat tile, a configuration summary. Signals "this is content
   * within the thing you opened", where a normal card would read as a peer of
   * it. Overrides `subtle`.
   */
  inset?: boolean;

  /**
   * When `true`, renders the card in its selected state: an accent border
   * (`border-border-accent`) over a translucent accent fill (`bg-accent/10`).
   *
   * Use for single-select card surfaces — e.g. composing `RadioGroup` +
   * `Card` where the card mirrors the group value. Composes with
   * `interactive`: the selected border takes precedence over the default
   * border while hover and focus-within treatments continue to apply.
   *
   * ```tsx
   * <Card interactive selected={value === option.value} onClick={() => setValue(option.value)}>
   *   …
   * </Card>
   * ```
   */
  selected?: boolean;

  /**
   * How the selected state is drawn. Only read when `selected` is `true`.
   *
   * - `'tint'` (default) — accent border over a translucent accent fill.
   *   For compact single-select option cards (radio-card composition).
   * - `'outline'` — accent border over `bg-surface-elevated`, no fill.
   *   For large container cards (expandable pickers, multi-select trees)
   *   where a tinted wash over the whole body would overwhelm the content.
   */
  selectedVariant?: 'tint' | 'outline';

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

  /**
   * When `true`, uses `bg-surface-elevated` instead of `bg-surface`.
   * Use for summary or detail cards that sit inside a modal or panel
   * where you want a subtle visual distinction from the surrounding surface.
   */
  subtle?: boolean;

  /**
   * Border colour token.
   * - `'subtle'` (default) — `border-border-subtle`, the standard card border.
   * - `'default'` — `border-border`, matching the Input component border.
   *   Use when the card sits adjacent to an Input and visual parity matters.
   */
  border?: 'subtle' | 'default';

  /**
   * When `true`, adds `divide-y divide-border-subtle` between direct children.
   * Use for list-style card bodies where each child represents a row.
   *
   * ```tsx
   * <Card divide>
   *   <Row>…</Row>
   *   <Row>…</Row>
   * </Card>
   * ```
   */
  divide?: boolean;
}

const Card = React.forwardRef<HTMLDivElement, CardProps>(
  (
    {
      className,
      column = false,
      interactive = false,
      inset = false,
      selected = false,
      selectedVariant = 'tint',
      overflowY,
      maxHeight,
      toggleable = false,
      defaultOpen = false,
      subtle = false,
      border = 'subtle',
      divide = false,
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
        data-selected={selected ? 'true' : undefined}
        className={cn(
          'rounded-lg border text-foreground overflow-hidden',
          border === 'default' ? 'border-border' : 'border-border-subtle',
          inset ? 'bg-background' : subtle ? 'bg-surface-elevated' : 'bg-surface',
          selected &&
            (selectedVariant === 'outline'
              ? 'border-border-accent bg-surface-elevated'
              : 'border-border-accent bg-accent/10'),
          (column || toggleable) && 'flex flex-col',
          divide && 'divide-y divide-border-subtle',
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
          'flex flex-col gap-1.5 px-6',
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
  /** @internal Used for internal Hades composition via cn(). */
  className?: string;
  id?: string;
  'aria-label'?: string;
}

/**
 * CardTitle
 *
 * Primary heading inside a `CardHeader`. Delegates directly to the Hades
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
  /** @internal Used for internal Hades composition via cn(). */
  className?: string;
  id?: string;
  'aria-label'?: string;
}

/**
 * CardDescription
 *
 * Supporting text beneath `CardTitle`. Delegates to the Hades `Text`
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
  /**
   * When `true`, reduces padding from `px-6 py-6` to `px-3 py-3`.
   * Use for summary or detail cards inside modals where the standard spacing
   * feels too generous relative to the compact context.
   *
   * ```tsx
   * <Card subtle>
   *   <CardContent padded compact>…</CardContent>
   * </Card>
   * ```
   */
  compact?: boolean;
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
  ({ className, padded = false, flush = false, grow = false, compact = false, ...props }, ref) => {
    const ctx = React.useContext(CardContext);
    if (ctx && !ctx.open) return null;
    return (
      <div
        ref={ref}
        className={cn(
          flush
            ? 'p-0'
            : compact
              ? cn('px-3 pb-3', padded ? 'pt-3' : 'pt-0')
              : cn('px-6 pb-6', padded ? 'pt-6' : 'pt-0'),
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
 * Rendered automatically by `<Card toggleable>`. Internal to Hades — not part
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
// CardHeaderProps, CardProps, CardTitleProps, CardDescriptionProps, and
// CardContentProps are exported at their declaration sites (`export interface`).
// CardFooterProps is declared as a plain `interface`, so it needs the re-export.
export type { CardFooterProps };
