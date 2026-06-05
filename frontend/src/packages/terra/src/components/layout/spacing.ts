/**
 * Terra Layout Spacing Tokens
 * ============================
 * Shared spacing scale for `Stack` and `Grid`. Consumed internally — only the
 * `SpacingSize` and `Responsive` types are re-exported from the package barrel.
 *
 * `SpacingSize` maps semantic feel-names to Tailwind's built-in spacing scale;
 * no custom CSS properties are introduced.
 *
 *   none        →   0px  (gap-0)
 *   tight       →   4px  (gap-1)
 *   snug        →   8px  (gap-2)
 *   cozy        →  12px  (gap-3)
 *   comfortable →  16px  (gap-4)
 *   relaxed     →  24px  (gap-6)
 *   loose       →  32px  (gap-8)
 *   spacious    →  40px  (gap-10)
 */

// ── Public types ──────────────────────────────────────────────────────────────

export type Breakpoint = 'base' | 'sm' | 'md' | 'lg' | 'xl';

/**
 * Responsive&lt;T&gt;
 *
 * Pass a single value to apply it at every viewport width, or a breakpoint
 * object for per-breakpoint overrides. Follows Tailwind's mobile-first
 * convention: each key activates at that breakpoint and above.
 *
 * ```tsx
 * // Same gap at every size
 * &lt;Stack gap="comfortable"&gt;…&lt;/Stack&gt;
 *
 * // Snug on mobile, comfortable on medium+
 * &lt;Stack gap={{ base: 'snug', md: 'comfortable' }}&gt;…&lt;/Stack&gt;
 * ```
 */
export type Responsive<T> = T | Partial<Record<Breakpoint, T>>;

/**
 * Named spacing scale — describes feel, not pixel size.
 *
 * Used as the `gap`, `gapX`, and `gapY` prop type on `Grid`,
 * and the `gap` prop type on `Stack`.
 */
export type SpacingSize =
  | 'none'
  | 'tight'
  | 'snug'
  | 'cozy'
  | 'comfortable'
  | 'relaxed'
  | 'loose'
  | 'spacious';

// ── Internal constants ────────────────────────────────────────────────────────
// Not re-exported from the package barrel — internal to the layout directory.

export const BREAKPOINTS: Breakpoint[] = ['base', 'sm', 'md', 'lg', 'xl'];

// ── Internal lookup tables ────────────────────────────────────────────────────
// Every utility class must appear as a full string literal so Tailwind v4's
// static scanner finds them. Never build class names via template literals.

/** gap-* — uniform gap, used by both Stack and Grid */
export const GAP: Record<Breakpoint, Record<SpacingSize, string>> = {
  base: {
    none: 'gap-0',
    tight: 'gap-1',
    snug: 'gap-2',
    cozy: 'gap-3',
    comfortable: 'gap-4',
    relaxed: 'gap-6',
    loose: 'gap-8',
    spacious: 'gap-10',
  },
  sm: {
    none: 'sm:gap-0',
    tight: 'sm:gap-1',
    snug: 'sm:gap-2',
    cozy: 'sm:gap-3',
    comfortable: 'sm:gap-4',
    relaxed: 'sm:gap-6',
    loose: 'sm:gap-8',
    spacious: 'sm:gap-10',
  },
  md: {
    none: 'md:gap-0',
    tight: 'md:gap-1',
    snug: 'md:gap-2',
    cozy: 'md:gap-3',
    comfortable: 'md:gap-4',
    relaxed: 'md:gap-6',
    loose: 'md:gap-8',
    spacious: 'md:gap-10',
  },
  lg: {
    none: 'lg:gap-0',
    tight: 'lg:gap-1',
    snug: 'lg:gap-2',
    cozy: 'lg:gap-3',
    comfortable: 'lg:gap-4',
    relaxed: 'lg:gap-6',
    loose: 'lg:gap-8',
    spacious: 'lg:gap-10',
  },
  xl: {
    none: 'xl:gap-0',
    tight: 'xl:gap-1',
    snug: 'xl:gap-2',
    cozy: 'xl:gap-3',
    comfortable: 'xl:gap-4',
    relaxed: 'xl:gap-6',
    loose: 'xl:gap-8',
    spacious: 'xl:gap-10',
  },
};

/** gap-x-* — horizontal (column) gap override, Grid only */
export const GAP_X: Record<Breakpoint, Record<SpacingSize, string>> = {
  base: {
    none: 'gap-x-0',
    tight: 'gap-x-1',
    snug: 'gap-x-2',
    cozy: 'gap-x-3',
    comfortable: 'gap-x-4',
    relaxed: 'gap-x-6',
    loose: 'gap-x-8',
    spacious: 'gap-x-10',
  },
  sm: {
    none: 'sm:gap-x-0',
    tight: 'sm:gap-x-1',
    snug: 'sm:gap-x-2',
    cozy: 'sm:gap-x-3',
    comfortable: 'sm:gap-x-4',
    relaxed: 'sm:gap-x-6',
    loose: 'sm:gap-x-8',
    spacious: 'sm:gap-x-10',
  },
  md: {
    none: 'md:gap-x-0',
    tight: 'md:gap-x-1',
    snug: 'md:gap-x-2',
    cozy: 'md:gap-x-3',
    comfortable: 'md:gap-x-4',
    relaxed: 'md:gap-x-6',
    loose: 'md:gap-x-8',
    spacious: 'md:gap-x-10',
  },
  lg: {
    none: 'lg:gap-x-0',
    tight: 'lg:gap-x-1',
    snug: 'lg:gap-x-2',
    cozy: 'lg:gap-x-3',
    comfortable: 'lg:gap-x-4',
    relaxed: 'lg:gap-x-6',
    loose: 'lg:gap-x-8',
    spacious: 'lg:gap-x-10',
  },
  xl: {
    none: 'xl:gap-x-0',
    tight: 'xl:gap-x-1',
    snug: 'xl:gap-x-2',
    cozy: 'xl:gap-x-3',
    comfortable: 'xl:gap-x-4',
    relaxed: 'xl:gap-x-6',
    loose: 'xl:gap-x-8',
    spacious: 'xl:gap-x-10',
  },
};

/** gap-y-* — vertical (row) gap override, Grid only */
export const GAP_Y: Record<Breakpoint, Record<SpacingSize, string>> = {
  base: {
    none: 'gap-y-0',
    tight: 'gap-y-1',
    snug: 'gap-y-2',
    cozy: 'gap-y-3',
    comfortable: 'gap-y-4',
    relaxed: 'gap-y-6',
    loose: 'gap-y-8',
    spacious: 'gap-y-10',
  },
  sm: {
    none: 'sm:gap-y-0',
    tight: 'sm:gap-y-1',
    snug: 'sm:gap-y-2',
    cozy: 'sm:gap-y-3',
    comfortable: 'sm:gap-y-4',
    relaxed: 'sm:gap-y-6',
    loose: 'sm:gap-y-8',
    spacious: 'sm:gap-y-10',
  },
  md: {
    none: 'md:gap-y-0',
    tight: 'md:gap-y-1',
    snug: 'md:gap-y-2',
    cozy: 'md:gap-y-3',
    comfortable: 'md:gap-y-4',
    relaxed: 'md:gap-y-6',
    loose: 'md:gap-y-8',
    spacious: 'md:gap-y-10',
  },
  lg: {
    none: 'lg:gap-y-0',
    tight: 'lg:gap-y-1',
    snug: 'lg:gap-y-2',
    cozy: 'lg:gap-y-3',
    comfortable: 'lg:gap-y-4',
    relaxed: 'lg:gap-y-6',
    loose: 'lg:gap-y-8',
    spacious: 'lg:gap-y-10',
  },
  xl: {
    none: 'xl:gap-y-0',
    tight: 'xl:gap-y-1',
    snug: 'xl:gap-y-2',
    cozy: 'xl:gap-y-3',
    comfortable: 'xl:gap-y-4',
    relaxed: 'xl:gap-y-6',
    loose: 'xl:gap-y-8',
    spacious: 'xl:gap-y-10',
  },
};

// ── Padding / margin class maps (non-responsive) ──────────────────────────────
// These are scalar lookups used by Stack's padding/margin props.
// Full string literals required for Tailwind v4's static scanner.

/** All-sides padding: p-* */
export const PADDING: Record<SpacingSize, string> = {
  none: 'p-0',
  tight: 'p-1',
  snug: 'p-2',
  cozy: 'p-3',
  comfortable: 'p-4',
  relaxed: 'p-6',
  loose: 'p-8',
  spacious: 'p-10',
};

/** Block (vertical) padding: py-* */
export const PADDING_BLOCK: Record<SpacingSize, string> = {
  none: 'py-0',
  tight: 'py-1',
  snug: 'py-2',
  cozy: 'py-3',
  comfortable: 'py-4',
  relaxed: 'py-6',
  loose: 'py-8',
  spacious: 'py-10',
};

/** Inline (horizontal) padding: px-* */
export const PADDING_INLINE: Record<SpacingSize, string> = {
  none: 'px-0',
  tight: 'px-1',
  snug: 'px-2',
  cozy: 'px-3',
  comfortable: 'px-4',
  relaxed: 'px-6',
  loose: 'px-8',
  spacious: 'px-10',
};

/** Top padding: pt-* */
export const PADDING_TOP: Record<SpacingSize, string> = {
  none: 'pt-0',
  tight: 'pt-1',
  snug: 'pt-2',
  cozy: 'pt-3',
  comfortable: 'pt-4',
  relaxed: 'pt-6',
  loose: 'pt-8',
  spacious: 'pt-10',
};

/** Bottom padding: pb-* */
export const PADDING_BOTTOM: Record<SpacingSize, string> = {
  none: 'pb-0',
  tight: 'pb-1',
  snug: 'pb-2',
  cozy: 'pb-3',
  comfortable: 'pb-4',
  relaxed: 'pb-6',
  loose: 'pb-8',
  spacious: 'pb-10',
};

/** Top margin: mt-* */
export const MARGIN_TOP: Record<SpacingSize, string> = {
  none: 'mt-0',
  tight: 'mt-1',
  snug: 'mt-2',
  cozy: 'mt-3',
  comfortable: 'mt-4',
  relaxed: 'mt-6',
  loose: 'mt-8',
  spacious: 'mt-10',
};

/** Bottom margin: mb-* */
export const MARGIN_BOTTOM: Record<SpacingSize, string> = {
  none: 'mb-0',
  tight: 'mb-1',
  snug: 'mb-2',
  cozy: 'mb-3',
  comfortable: 'mb-4',
  relaxed: 'mb-6',
  loose: 'mb-8',
  spacious: 'mb-10',
};

/** Inline (horizontal) margin: mx-* */
export const MARGIN_INLINE: Record<SpacingSize, string> = {
  none: 'mx-0',
  tight: 'mx-1',
  snug: 'mx-2',
  cozy: 'mx-3',
  comfortable: 'mx-4',
  relaxed: 'mx-6',
  loose: 'mx-8',
  spacious: 'mx-10',
};

// ── Internal helper ───────────────────────────────────────────────────────────

/**
 * Resolves a `Responsive<T>` value into one or more Tailwind class strings.
 * A scalar maps to the base-breakpoint class; an object emits one prefixed
 * class per key.
 *
 * @internal Not part of the public package API.
 */
export function resolveResponsive<T extends PropertyKey>(
  value: Responsive<T> | undefined,
  table: Record<Breakpoint, Record<T, string>>,
  fallback: T,
): string {
  const v = value ?? fallback;

  if (typeof v === 'string' || typeof v === 'number') {
    return table.base[v as T];
  }

  const obj = v as Partial<Record<Breakpoint, T>>;
  return BREAKPOINTS.filter((bp) => obj[bp] !== undefined)
    .map((bp) => table[bp][obj[bp]!])
    .join(' ');
}
