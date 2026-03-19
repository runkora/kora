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
 *   comfortable →  16px  (gap-4)
 *   relaxed     →  24px  (gap-6)
 *   loose       →  32px  (gap-8)
 *   spacious    →  48px  (gap-12)
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
  base: { none: 'gap-0', tight: 'gap-1', snug: 'gap-2', comfortable: 'gap-4', relaxed: 'gap-6', loose: 'gap-8', spacious: 'gap-12' },
  sm:   { none: 'sm:gap-0', tight: 'sm:gap-1', snug: 'sm:gap-2', comfortable: 'sm:gap-4', relaxed: 'sm:gap-6', loose: 'sm:gap-8', spacious: 'sm:gap-12' },
  md:   { none: 'md:gap-0', tight: 'md:gap-1', snug: 'md:gap-2', comfortable: 'md:gap-4', relaxed: 'md:gap-6', loose: 'md:gap-8', spacious: 'md:gap-12' },
  lg:   { none: 'lg:gap-0', tight: 'lg:gap-1', snug: 'lg:gap-2', comfortable: 'lg:gap-4', relaxed: 'lg:gap-6', loose: 'lg:gap-8', spacious: 'lg:gap-12' },
  xl:   { none: 'xl:gap-0', tight: 'xl:gap-1', snug: 'xl:gap-2', comfortable: 'xl:gap-4', relaxed: 'xl:gap-6', loose: 'xl:gap-8', spacious: 'xl:gap-12' },
};

/** gap-x-* — horizontal (column) gap override, Grid only */
export const GAP_X: Record<Breakpoint, Record<SpacingSize, string>> = {
  base: { none: 'gap-x-0', tight: 'gap-x-1', snug: 'gap-x-2', comfortable: 'gap-x-4', relaxed: 'gap-x-6', loose: 'gap-x-8', spacious: 'gap-x-12' },
  sm:   { none: 'sm:gap-x-0', tight: 'sm:gap-x-1', snug: 'sm:gap-x-2', comfortable: 'sm:gap-x-4', relaxed: 'sm:gap-x-6', loose: 'sm:gap-x-8', spacious: 'sm:gap-x-12' },
  md:   { none: 'md:gap-x-0', tight: 'md:gap-x-1', snug: 'md:gap-x-2', comfortable: 'md:gap-x-4', relaxed: 'md:gap-x-6', loose: 'md:gap-x-8', spacious: 'md:gap-x-12' },
  lg:   { none: 'lg:gap-x-0', tight: 'lg:gap-x-1', snug: 'lg:gap-x-2', comfortable: 'lg:gap-x-4', relaxed: 'lg:gap-x-6', loose: 'lg:gap-x-8', spacious: 'lg:gap-x-12' },
  xl:   { none: 'xl:gap-x-0', tight: 'xl:gap-x-1', snug: 'xl:gap-x-2', comfortable: 'xl:gap-x-4', relaxed: 'xl:gap-x-6', loose: 'xl:gap-x-8', spacious: 'xl:gap-x-12' },
};

/** gap-y-* — vertical (row) gap override, Grid only */
export const GAP_Y: Record<Breakpoint, Record<SpacingSize, string>> = {
  base: { none: 'gap-y-0', tight: 'gap-y-1', snug: 'gap-y-2', comfortable: 'gap-y-4', relaxed: 'gap-y-6', loose: 'gap-y-8', spacious: 'gap-y-12' },
  sm:   { none: 'sm:gap-y-0', tight: 'sm:gap-y-1', snug: 'sm:gap-y-2', comfortable: 'sm:gap-y-4', relaxed: 'sm:gap-y-6', loose: 'sm:gap-y-8', spacious: 'sm:gap-y-12' },
  md:   { none: 'md:gap-y-0', tight: 'md:gap-y-1', snug: 'md:gap-y-2', comfortable: 'md:gap-y-4', relaxed: 'md:gap-y-6', loose: 'md:gap-y-8', spacious: 'md:gap-y-12' },
  lg:   { none: 'lg:gap-y-0', tight: 'lg:gap-y-1', snug: 'lg:gap-y-2', comfortable: 'lg:gap-y-4', relaxed: 'lg:gap-y-6', loose: 'lg:gap-y-8', spacious: 'lg:gap-y-12' },
  xl:   { none: 'xl:gap-y-0', tight: 'xl:gap-y-1', snug: 'xl:gap-y-2', comfortable: 'xl:gap-y-4', relaxed: 'xl:gap-y-6', loose: 'xl:gap-y-8', spacious: 'xl:gap-y-12' },
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
  return BREAKPOINTS
    .filter(bp => obj[bp] !== undefined)
    .map(bp => table[bp][obj[bp]!])
    .join(' ');
}
