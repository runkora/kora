import * as React from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

// ── Heading ───────────────────────────────────────────────────────────────────

/**
 * Heading visual variants — named by their role in the UI hierarchy, not by
 * scale. The rendered HTML element is an implementation detail that follows
 * the variant by default and can be overridden with `asChild`.
 *
 *   display    — hero-scale wordmark or feature headline (h1); use on splash
 *                screens and landing views where one oversized title anchors
 *                the entire composition. text-5xl, tracking-tighter.
 *   title      — the single page-level landmark per view (h1). text-3xl.
 *   section    — major section heading; the most common heading level (h2).
 *   subsection — secondary grouping within a section (h3).
 *   label      — compact group label, card title, or sidebar header (h4).
 *   metric     — large display number for stat cards and KPI widgets (div);
 *                renders as a div since metric values are not structural
 *                landmarks. Always tabular-nums.
 */
const headingVariants = cva('font-sans tracking-tight', {
  variants: {
    variant: {
      /**
       * Display — hero-scale text for splash screens and landing pages.
       * Use when a single oversized title anchors the entire view.
       * Renders as h1.
       *
       * ```tsx
       * <Heading variant="display">Orbit</Heading>
       * ```
       */
      display: 'text-5xl font-bold leading-none tracking-tighter',
      /** Page-level title — reserved for a single landmark per view. */
      title: 'text-3xl font-bold leading-tight',
      /** Major section heading — the most common heading level. */
      section: 'text-2xl font-semibold leading-tight',
      /** Secondary grouping within a section. */
      subsection: 'text-xl font-semibold leading-snug',
      /** Compact group label, card title, or sidebar item header. */
      label: 'text-base font-semibold leading-snug',
      /**
       * Metric — large display number for stat cards and KPI widgets.
       * Renders as a div (semantic neutral) since metric values are not
       * structural headings. Always tabular-nums to prevent layout jitter as
       * values update.
       *
       * ```tsx
       * <Heading variant="metric">4,821</Heading>
       * ```
       */
      metric: 'text-3xl font-bold leading-none tabular-nums',
    },
    color: {
      /** Full-brightness heading — default. */
      foreground: 'text-foreground',
      /** Slightly dimmed — use for secondary headings. */
      muted: 'text-muted',
      /** Accent-coloured heading — use sparingly. */
      accent: 'text-accent',
    },
  },
  defaultVariants: {
    variant: 'section',
    color: 'foreground',
  },
});

/**
 * Default HTML element for each heading variant.
 *
 *   display   -> h1   (hero landmark)
 *   title     -> h1   (page landmark)
 *   section   -> h2
 *   subsection-> h3
 *   label     -> h4
 *   metric    -> div  (display number, not a structural landmark)
 */
const HEADING_TAG: Record<
  NonNullable<VariantProps<typeof headingVariants>['variant']>,
  React.ElementType
> = {
  display: 'h1',
  title: 'h1',
  section: 'h2',
  subsection: 'h3',
  label: 'h4',
  metric: 'div',
};

export interface HeadingProps
  extends Omit<React.HTMLAttributes<HTMLElement>, 'color'>, VariantProps<typeof headingVariants> {
  /**
   * When true, merges Heading styles into the immediate child element instead
   * of wrapping it. Useful for rendering a heading as a router Link.
   */
  asChild?: boolean;
}

/**
 * Heading
 *
 * Section headings named by their role in the UI hierarchy. The rendered HTML
 * element defaults to the appropriate h1-h4 tag for that variant but can be
 * overridden via `asChild` without losing visual styling.
 *
 * Use `variant="display"` for hero-scale text on splash/landing screens.
 * Use `variant="metric"` for large KPI/stat numbers — renders as a div.
 *
 * ```tsx
 * <Heading variant="display">Orbit</Heading>
 * <Heading variant="title">Agents</Heading>
 * <Heading variant="metric">4,821</Heading>
 * <Heading variant="subsection" color="muted">Configuration</Heading>
 * ```
 */
const Heading = React.forwardRef<HTMLElement, HeadingProps>(
  ({ className, variant = 'section', color, asChild = false, ...props }, ref) => {
    const Comp = (asChild ? Slot : HEADING_TAG[variant ?? 'section']) as React.ElementType;
    return (
      <Comp ref={ref} className={cn(headingVariants({ variant, color }), className)} {...props} />
    );
  },
);

Heading.displayName = 'Heading';

// ── Text ──────────────────────────────────────────────────────────────────────

/**
 * General-purpose body copy. Covers the full spectrum from large introductory
 * paragraphs down to fine-print captions, with color, weight, transform,
 * alignment, and overflow modifiers.
 *
 * Size variants use semantic names describing purpose, not scale:
 *   micro      — finest detail: 11px, timestamps in dense UIs, disclaimer text
 *   caption    — fine detail: 12px, timestamps, badge labels, auxiliary metadata
 *   supporting — secondary body: descriptions, helper text, table metadata
 *   body       — default prose: the majority of UI text lives here
 *   lead       — introductory: opening paragraphs, feature descriptions
 */
const textVariants = cva('font-sans', {
  variants: {
    size: {
      /**
       * Micro — 11px, for extremely dense UI contexts such as chat timestamps,
       * keyboard shortcut hints, and single-line disclaimer text where caption
       * (12px) would still feel too large. Use sparingly.
       */
      micro: 'text-[11px] leading-none',
      /** Finest text — 12px timestamps, status labels, auxiliary data. */
      caption: 'text-xs leading-none',
      /** Secondary body — descriptions, helper text, table metadata. */
      supporting: 'text-sm leading-normal',
      /** Default prose — the primary body copy size. */
      body: 'text-base leading-normal',
      /** Introductory paragraphs — lead text above sections or features. */
      lead: 'text-lg leading-relaxed',
    },
    color: {
      /** Primary content — full-brightness. */
      foreground: 'text-foreground',
      /** Secondary content — dimmed one step. */
      muted: 'text-muted',
      /** Tertiary content — dimmed two steps. */
      subtle: 'text-subtle',
      /** Highlighted or interactive text. */
      accent: 'text-accent',
      /**
       * Status colours — use for metric deltas, health indicators, and inline
       * status strings that must match the badge/progress token colours.
       * Prefer a Badge component for standalone status labels.
       */
      'status-healthy': 'text-status-healthy',
      'status-degraded': 'text-status-degraded',
      'status-error': 'text-status-error',
    },
    weight: {
      /** Normal prose — default reading weight. */
      regular: 'font-normal',
      /** Mild emphasis — labels, form field text. */
      emphasis: 'font-medium',
      /** Strong emphasis — column headers, key values. */
      strong: 'font-semibold',
      /** Maximum weight — reserved for deliberate typographic impact. */
      heavy: 'font-bold',
    },
    /**
     * Text transform — use `uppercase` for eyebrow labels, group headings,
     * and section dividers that follow the ALL-CAPS convention.
     * Only specify when the uppercase treatment is semantically meaningful
     * (e.g. a category label), not for decorative purposes.
     */
    transform: {
      /** No transform — default. */
      none: '',
      /**
       * Uppercase + widened tracking — for compact eyebrow labels and sidebar
       * group headings. Pairs well with size="caption" and weight="strong".
       *
       * ```tsx
       * <Text size="caption" weight="strong" transform="uppercase" color="subtle">
       *   Today
       * </Text>
       * ```
       */
      uppercase: 'uppercase tracking-widest',
    },
    /**
     * Text alignment — controls horizontal alignment of the rendered block.
     * Prefer wrapper-level alignment where possible; use this prop when the
     * text element itself must control its own alignment (e.g. centred
     * disclaimers, right-aligned metadata).
     */
    align: {
      /** Left-aligned — default for LTR prose. */
      start: 'text-left',
      /** Centred — for single-line labels, disclaimers, empty states. */
      center: 'text-center',
      /** Right-aligned — for numeric columns and trailing metadata. */
      end: 'text-right',
    },
  },
  defaultVariants: {
    size: 'body',
    color: 'foreground',
    weight: 'regular',
  },
});

export interface TextProps
  extends Omit<React.HTMLAttributes<HTMLElement>, 'color'>, VariantProps<typeof textVariants> {
  /**
   * Override the rendered HTML element. Restricted to phrasing-content tags
   * where a block `<p>` would produce invalid HTML — for example inside a
   * `<button>`, `<a>`, `<li>`, or form label.
   *
   * ```tsx
   * {/* inside a <button> — renders a <span>, not a <p> *\/}
   * <Text as="span" size="supporting" weight="emphasis">{title}</Text>
   * ```
   *
   * Deliberately NOT `React.ElementType` — layout wrappers belong to Stack
   * and Grid, not Text. Use `asChild` if you need to merge styles onto a
   * child element you already control.
   */
  as?: 'span' | 'li' | 'dt' | 'dd' | 'label' | 'legend';

  /**
   * When true, merges Text styles into the immediate child element instead of
   * rendering a p tag.
   */
  asChild?: boolean;

  /**
   * When true, prevents text from wrapping and truncates with an ellipsis
   * when the content overflows its container. The containing element must
   * have a defined width for truncation to take effect.
   *
   * Use this instead of className="truncate" when the overflow behaviour
   * is intrinsic to the content itself (e.g. a filename, a conversation
   * title, a long agent ID).
   *
   * ```tsx
   * <Text size="caption" weight="emphasis" truncate>
   *   A very long conversation title that will be clipped
   * </Text>
   * ```
   */
  truncate?: boolean;

  /**
   * When true, renders the text in a monospace font (font-mono). Use for
   * technical identifiers such as agent IDs, version strings, provider names,
   * run counts, and timestamps that benefit from fixed-width characters.
   *
   * Prefer the Code component for inline technical strings inside prose.
   * Use `mono` on Text when the entire cell or label is a technical value.
   *
   * ```tsx
   * <Text size="supporting" mono>apollo-retriever</Text>
   * <Text size="caption" color="subtle" mono>agt_01HZ9K3V</Text>
   * ```
   */
  mono?: boolean;

  /**
   * When true, applies tabular-nums to prevent layout jitter when numeric
   * content updates (e.g. counters, run totals, timestamps). Use alongside
   * `mono` for the best fixed-width numeric rendering.
   *
   * ```tsx
   * <Text size="supporting" mono tabular>{runs.toLocaleString()}</Text>
   * ```
   */
  tabular?: boolean;
}

/**
 * Text
 *
 * The workhorse of the type system — use this for all body copy, metadata,
 * captions, and descriptive strings. Compose size, color, weight, transform,
 * align, truncate, mono, and tabular props to cover every prose context
 * without writing custom Tailwind classes.
 *
 * Renders a `p` by default. Use `as="span"` to render as an inline element
 * (e.g. inside a `<button>` or `<a>` where a block `<p>` is invalid HTML).
 * Use `asChild` to merge styles onto an existing child element.
 *
 * ```tsx
 * <Text>Agent is running.</Text>
 * <Text size="supporting" color="muted">Last seen 2 minutes ago</Text>
 * <Text size="caption" color="subtle" weight="strong" transform="uppercase">Today</Text>
 * <Text size="caption" align="center" color="subtle">Orbit AI can make mistakes.</Text>
 * <Text size="micro" color="subtle">Shift + Enter for new line</Text>
 * <Text size="caption" weight="emphasis" truncate>Very long agent ID</Text>
 * <Text size="supporting" mono>apollo-retriever</Text>
 * <Text size="supporting" mono tabular>{runs.toLocaleString()}</Text>
 * <Text size="caption" color="status-healthy">+2 this hour</Text>
 * ```
 */
const Text = React.forwardRef<HTMLElement, TextProps>(
  (
    {
      className,
      size,
      color,
      weight,
      transform,
      align,
      truncate = false,
      mono = false,
      tabular = false,
      asChild = false,
      as,
      ...props
    },
    ref,
  ) => {
    // Cast to React.ElementType so TypeScript doesn't try to intersect the ref
    // types for every element in the `as` string-literal union.
    const Comp = (asChild ? Slot : (as ?? 'p')) as React.ElementType;
    return (
      <Comp
        ref={ref}
        className={cn(
          textVariants({ size, color, weight, transform, align }),
          truncate && 'truncate',
          mono && 'font-mono',
          tabular && 'tabular-nums',
          className,
        )}
        {...props}
      />
    );
  },
);

Text.displayName = 'Text';

// ── Code ──────────────────────────────────────────────────────────────────────

/**
 * Inline monospace text for identifiers, token names, agent IDs, file paths,
 * and any other technical string that benefits from fixed-width rendering.
 *
 * Variant names describe context of use, not appearance:
 *   inline     — inside prose, clearly distinguished from surrounding text
 *   bare       — inside a component that already provides container styling
 *   highlight  — a named identifier that deserves deliberate visual emphasis
 */
const codeVariants = cva('font-mono rounded-sm px-1 py-0.5 text-sm', {
  variants: {
    variant: {
      /**
       * Inline — subtle surface background distinguishes the span from prose.
       * Use inside sentences and paragraphs.
       */
      inline: 'bg-surface-elevated text-foreground',
      /**
       * Bare — monospace font only, no background. Use inside tables, badges,
       * or other components that already provide a visual container.
       */
      bare: 'text-muted',
      /**
       * Highlight — draws attention to a specific identifier (e.g., a key
       * token name, an active agent ID, a critical env variable).
       */
      highlight: 'text-accent',
    },
  },
  defaultVariants: {
    variant: 'inline',
  },
});

export interface CodeProps
  extends React.HTMLAttributes<HTMLElement>, VariantProps<typeof codeVariants> {}

/**
 * Code
 *
 * Inline monospace spans for technical identifiers. Use in prose, table cells,
 * and documentation components to ensure agent IDs, variable names, and paths
 * are rendered with consistent typographic treatment.
 *
 * Always renders a code element for correct semantic HTML.
 *
 * ```tsx
 * <Code>agent-7f2a3b</Code>
 * <Code variant="bare">--color-accent</Code>
 * <Code variant="highlight">ORBIT_API_KEY</Code>
 * ```
 */
const Code = React.forwardRef<HTMLElement, CodeProps>(({ className, variant, ...props }, ref) => (
  <code ref={ref} className={cn(codeVariants({ variant }), className)} {...props} />
));

Code.displayName = 'Code';

// ── Exports ───────────────────────────────────────────────────────────────────

export { Heading, headingVariants, Text, textVariants, Code, codeVariants };
