/**
 * Terra Layout Sizing Tokens
 * ==========================
 * Semantic width, height, and text-alignment constraints for `Stack` and `Grid`.
 * All lookup tables are full string literals — Tailwind v4's static scanner
 * cannot resolve template literals or dynamic class construction.
 *
 * ## ContainerWidth
 *
 *   compact   →  max-w-xs     (320px  — status widgets, tight callouts)
 *   card      →  max-w-sm     (384px  — standard card column)
 *   prose     →  max-w-prose  (65ch   — subtitle, article, readable text)
 *   document  →  max-w-2xl    (672px  — AI chat canvas, doc editor)
 *   wide      →  max-w-4xl    (896px  — dashboard panels, data tables)
 *   full      →  max-w-full   (100%   — fills the nearest container)
 *
 * ## ContainerHeight
 *
 *   screen    →  min-h-dvh / max-h-dvh  (viewport height, mobile-safe dvh units)
 *   full      →  min-h-full / max-h-full (100% of parent)
 */

import type { Breakpoint } from './spacing';

// ── Public types ──────────────────────────────────────────────────────────────

/**
 * Semantic width scale for `maxWidth` and `minWidth` props on `Stack` and `Grid`.
 *
 * Values describe purpose, not pixel size.
 */
export type ContainerWidth = 'compact' | 'card' | 'prose' | 'document' | 'wide' | 'full';

/**
 * Semantic height scale for `maxHeight` and `minHeight` props on `Stack` and `Grid`.
 *
 * `'screen'` uses `dvh` units (dynamic viewport height) — correct on mobile browsers
 * where the address bar changes height. Prefer this over `vh`.
 */
export type ContainerHeight = 'screen' | 'full';

/**
 * Text alignment for the `textAlign` prop on `Stack`.
 *
 * Maps to Tailwind's `text-*` utilities. Omitting the prop inherits alignment
 * from the parent (no class emitted).
 */
export type TextAlign = 'start' | 'center' | 'end' | 'justify';

// ── Internal lookup tables ────────────────────────────────────────────────────
// Exported for use in stack.tsx and grid.tsx only — not re-exported from the
// package barrel.

/** max-w-* per breakpoint */
export const MAX_W: Record<Breakpoint, Record<ContainerWidth, string>> = {
  base: {
    compact: 'max-w-xs',
    card: 'max-w-sm',
    prose: 'max-w-prose',
    document: 'max-w-2xl',
    wide: 'max-w-4xl',
    full: 'max-w-full',
  },
  sm: {
    compact: 'sm:max-w-xs',
    card: 'sm:max-w-sm',
    prose: 'sm:max-w-prose',
    document: 'sm:max-w-2xl',
    wide: 'sm:max-w-4xl',
    full: 'sm:max-w-full',
  },
  md: {
    compact: 'md:max-w-xs',
    card: 'md:max-w-sm',
    prose: 'md:max-w-prose',
    document: 'md:max-w-2xl',
    wide: 'md:max-w-4xl',
    full: 'md:max-w-full',
  },
  lg: {
    compact: 'lg:max-w-xs',
    card: 'lg:max-w-sm',
    prose: 'lg:max-w-prose',
    document: 'lg:max-w-2xl',
    wide: 'lg:max-w-4xl',
    full: 'lg:max-w-full',
  },
  xl: {
    compact: 'xl:max-w-xs',
    card: 'xl:max-w-sm',
    prose: 'xl:max-w-prose',
    document: 'xl:max-w-2xl',
    wide: 'xl:max-w-4xl',
    full: 'xl:max-w-full',
  },
};

/** min-w-* per breakpoint */
export const MIN_W: Record<Breakpoint, Record<ContainerWidth, string>> = {
  base: {
    compact: 'min-w-xs',
    card: 'min-w-sm',
    prose: 'min-w-prose',
    document: 'min-w-2xl',
    wide: 'min-w-4xl',
    full: 'min-w-full',
  },
  sm: {
    compact: 'sm:min-w-xs',
    card: 'sm:min-w-sm',
    prose: 'sm:min-w-prose',
    document: 'sm:min-w-2xl',
    wide: 'sm:min-w-4xl',
    full: 'sm:min-w-full',
  },
  md: {
    compact: 'md:min-w-xs',
    card: 'md:min-w-sm',
    prose: 'md:min-w-prose',
    document: 'md:min-w-2xl',
    wide: 'md:min-w-4xl',
    full: 'md:min-w-full',
  },
  lg: {
    compact: 'lg:min-w-xs',
    card: 'lg:min-w-sm',
    prose: 'lg:min-w-prose',
    document: 'lg:min-w-2xl',
    wide: 'lg:min-w-4xl',
    full: 'lg:min-w-full',
  },
  xl: {
    compact: 'xl:min-w-xs',
    card: 'xl:min-w-sm',
    prose: 'xl:min-w-prose',
    document: 'xl:min-w-2xl',
    wide: 'xl:min-w-4xl',
    full: 'xl:min-w-full',
  },
};

/** min-h-* per breakpoint */
export const MIN_H: Record<Breakpoint, Record<ContainerHeight, string>> = {
  base: { screen: 'min-h-dvh', full: 'min-h-full' },
  sm: { screen: 'sm:min-h-dvh', full: 'sm:min-h-full' },
  md: { screen: 'md:min-h-dvh', full: 'md:min-h-full' },
  lg: { screen: 'lg:min-h-dvh', full: 'lg:min-h-full' },
  xl: { screen: 'xl:min-h-dvh', full: 'xl:min-h-full' },
};

/** max-h-* per breakpoint */
export const MAX_H: Record<Breakpoint, Record<ContainerHeight, string>> = {
  base: { screen: 'max-h-dvh', full: 'max-h-full' },
  sm: { screen: 'sm:max-h-dvh', full: 'sm:max-h-full' },
  md: { screen: 'md:max-h-dvh', full: 'md:max-h-full' },
  lg: { screen: 'lg:max-h-dvh', full: 'lg:max-h-full' },
  xl: { screen: 'xl:max-h-dvh', full: 'xl:max-h-full' },
};

/**
 * text-* scalar — not responsive; text alignment rarely changes at breakpoints.
 * Omitting `textAlign` emits no class (inherits alignment from the parent).
 */
export const TEXT_ALIGN: Record<TextAlign, string> = {
  start: 'text-left',
  center: 'text-center',
  end: 'text-right',
  justify: 'text-justify',
};
