/**
 * What a theme can set that `tokens.css` does not say: token kinds, groups, labels, contrast pairs, slots and name bounds.
 * Every `--hades-raw-*` property is settable in both modes. Semantic names never are.
 */

export const MODES = ['dark', 'light'] as const;
export type Mode = (typeof MODES)[number];

export type TokenKind = 'color' | 'shadow' | 'length' | 'font';

/** The value syntax a theme may supply for each kind. */
export type TokenFormat = 'hex-or-oklch' | 'css-shadow-list' | 'css-length' | 'font-family-name';

export const FORMAT_FOR_KIND: Readonly<Record<TokenKind, TokenFormat>> = {
  color: 'hex-or-oklch',
  shadow: 'css-shadow-list',
  length: 'css-length',
  font: 'font-family-name',
};

/** A display group a consumer, such as a theme preview, lists tokens under. */
export interface GroupDeclaration {
  /** Consumers hard-code it; labels and order are display and may change freely. */
  readonly id: string;
  readonly label: string;
  /** Display order, ascending and unique. */
  readonly order: number;
}

/** The groups, in display order. */
export const GROUPS: readonly GroupDeclaration[] = [
  { id: 'branding', label: 'Branding', order: 1 },
  { id: 'neutral', label: 'Neutral ramp', order: 2 },
  { id: 'surfaces', label: 'Surfaces', order: 3 },
  { id: 'text', label: 'Text', order: 4 },
  { id: 'borders', label: 'Borders', order: 5 },
  { id: 'overlays', label: 'Overlays', order: 6 },
  { id: 'status', label: 'Status', order: 7 },
  { id: 'providers', label: 'Providers', order: 8 },
  { id: 'strategies', label: 'Strategies', order: 9 },
  { id: 'charts', label: 'Chart series', order: 10 },
  { id: 'radius', label: 'Radius', order: 11 },
  { id: 'shadows', label: 'Shadows', order: 12 },
  { id: 'fonts', label: 'Fonts', order: 13 },
  { id: 'motion', label: 'Motion', order: 14 },
];

/** Where a token or semantic name is listed, and what it is called there. */
export interface Placement {
  readonly group: string;
  readonly label: string;
}

export interface TokenDeclaration extends Placement {
  readonly kind: TokenKind;
}

const raw = (kind: TokenKind, group: string, label: string): TokenDeclaration => ({
  kind,
  group,
  label,
});
const at = (group: string, label: string): Placement => ({ group, label });

/** Kind, group and label of every raw token in `tokens.css`, by custom property name. */
export const TOKENS: Readonly<Record<string, TokenDeclaration>> = {
  // Neutral ramp
  '--hades-raw-black': raw('color', 'neutral', 'black'),
  '--hades-raw-950': raw('color', 'neutral', '950'),
  '--hades-raw-900': raw('color', 'neutral', '900'),
  '--hades-raw-800': raw('color', 'neutral', '800'),
  '--hades-raw-700': raw('color', 'neutral', '700'),
  '--hades-raw-600': raw('color', 'neutral', '600'),
  '--hades-raw-500': raw('color', 'neutral', '500'),
  '--hades-raw-400': raw('color', 'neutral', '400'),
  '--hades-raw-300': raw('color', 'neutral', '300'),
  '--hades-raw-200': raw('color', 'neutral', '200'),
  '--hades-raw-100': raw('color', 'neutral', '100'),
  '--hades-raw-white': raw('color', 'neutral', 'white'),

  // Accent ramp: the brand colours
  '--hades-raw-accent-600': raw('color', 'branding', 'accent 600'),
  '--hades-raw-accent-500': raw('color', 'branding', 'accent 500'),
  '--hades-raw-accent-400': raw('color', 'branding', 'accent 400'),
  '--hades-raw-accent-300': raw('color', 'branding', 'accent 300'),
  '--hades-raw-accent-fg': raw('color', 'branding', 'accent foreground'),

  // Status
  '--hades-raw-healthy': raw('color', 'status', 'healthy'),
  '--hades-raw-degraded': raw('color', 'status', 'degraded'),
  '--hades-raw-error': raw('color', 'status', 'error'),
  '--hades-raw-pending': raw('color', 'status', 'pending'),

  // Provider and strategy identity
  '--hades-raw-provider-bedrock': raw('color', 'providers', 'bedrock'),
  '--hades-raw-provider-vertexai': raw('color', 'providers', 'vertex ai'),
  '--hades-raw-provider-azure': raw('color', 'providers', 'azure'),
  '--hades-raw-strategy-weighted': raw('color', 'strategies', 'weighted'),

  // Chart series
  '--hades-raw-chart-1': raw('color', 'charts', 'series 1'),
  '--hades-raw-chart-2': raw('color', 'charts', 'series 2'),
  '--hades-raw-chart-3': raw('color', 'charts', 'series 3'),
  '--hades-raw-chart-4': raw('color', 'charts', 'series 4'),
  '--hades-raw-chart-5': raw('color', 'charts', 'series 5'),
  '--hades-raw-chart-6': raw('color', 'charts', 'series 6'),
  '--hades-raw-chart-7': raw('color', 'charts', 'series 7'),
  '--hades-raw-chart-8': raw('color', 'charts', 'series 8'),

  // Overlay
  '--hades-raw-overlay': raw('color', 'overlays', 'overlay'),
  '--hades-raw-overlay-strong': raw('color', 'overlays', 'overlay strong'),

  // Shadows
  '--hades-raw-shadow-sm': raw('shadow', 'shadows', 'small'),
  '--hades-raw-shadow-elevated': raw('shadow', 'shadows', 'elevated'),

  // Text and border minima
  '--hades-raw-subtle': raw('color', 'text', 'subtle'),
  '--hades-raw-border': raw('color', 'borders', 'border'),
  '--hades-raw-border-subtle': raw('color', 'borders', 'border subtle'),

  // Radius scale
  '--hades-raw-radius-sm': raw('length', 'radius', 'small'),
  '--hades-raw-radius-md': raw('length', 'radius', 'medium'),
  '--hades-raw-radius-lg': raw('length', 'radius', 'large'),
  '--hades-raw-radius-xl': raw('length', 'radius', 'extra large'),
  '--hades-raw-radius-2xl': raw('length', 'radius', '2x large'),

  // Font slots
  '--hades-raw-font-sans': raw('font', 'fonts', 'sans'),
  '--hades-raw-font-mono': raw('font', 'fonts', 'mono'),
};

/** Group and label of every semantic name in `@theme inline`. The group is the name's role, not its raw token's. */
export const SEMANTIC: Readonly<Record<string, Placement>> = {
  '--color-background': at('surfaces', 'background'),
  '--color-surface': at('surfaces', 'surface'),
  '--color-surface-elevated': at('surfaces', 'surface elevated'),
  '--color-surface-overlay': at('surfaces', 'surface overlay'),

  '--color-foreground': at('text', 'foreground'),
  '--color-muted': at('text', 'muted'),
  '--color-subtle': at('text', 'subtle'),

  '--color-border': at('borders', 'border'),
  '--color-border-subtle': at('borders', 'border subtle'),
  '--color-border-accent': at('borders', 'border accent'),

  '--color-accent': at('branding', 'accent'),
  '--color-accent-hover': at('branding', 'accent hover'),
  '--color-accent-foreground': at('branding', 'accent foreground'),
  '--color-accent-border': at('branding', 'accent border'),
  '--color-accent-muted': at('branding', 'accent muted'),
  '--color-accent-subtle': at('branding', 'accent subtle'),

  '--color-overlay': at('overlays', 'overlay'),
  '--color-overlay-strong': at('overlays', 'overlay strong'),

  '--color-status-healthy': at('status', 'healthy'),
  '--color-status-degraded': at('status', 'degraded'),
  '--color-status-error': at('status', 'error'),
  '--color-status-pending': at('status', 'pending'),
  '--color-status-idle': at('status', 'idle'),

  '--color-provider-bedrock': at('providers', 'bedrock'),
  '--color-provider-vertexai': at('providers', 'vertex ai'),
  '--color-provider-azure': at('providers', 'azure'),
  '--color-strategy-weighted': at('strategies', 'weighted'),

  '--color-chart-1': at('charts', 'series 1'),
  '--color-chart-2': at('charts', 'series 2'),
  '--color-chart-3': at('charts', 'series 3'),
  '--color-chart-4': at('charts', 'series 4'),
  '--color-chart-5': at('charts', 'series 5'),
  '--color-chart-6': at('charts', 'series 6'),
  '--color-chart-7': at('charts', 'series 7'),
  '--color-chart-8': at('charts', 'series 8'),

  '--radius-sm': at('radius', 'small'),
  '--radius-md': at('radius', 'medium'),
  '--radius-lg': at('radius', 'large'),
  '--radius-xl': at('radius', 'extra large'),
  '--radius-2xl': at('radius', '2x large'),
  '--radius-full': at('radius', 'full'),

  '--shadow-sm': at('shadows', 'small'),
  '--shadow-elevated': at('shadows', 'elevated'),

  '--font-sans': at('fonts', 'sans'),
  '--font-mono': at('fonts', 'mono'),

  '--ease-standard': at('motion', 'standard easing'),
};

/** How a failed contrast pair is reported: `error` fails the theme, `warn` is advisory. */
export type ContrastSeverity = 'error' | 'warn';

/** A text colour that keeps a minimum contrast on each background. Names are non-derived colours from `@theme inline`. */
export interface ContrastPair {
  /** Unique. */
  readonly id: string;
  readonly text: string;
  readonly on: readonly string[];
  /** WCAG 2.x contrast ratio, in [1, 21]. */
  readonly min: number;
  readonly severity: ContrastSeverity;
}

const SURFACE_NAMES = [
  '--color-background',
  '--color-surface',
  '--color-surface-elevated',
  '--color-surface-overlay',
] as const;

/**
 * Text pairs only, one id per text colour. Body text needs 4.5:1, status colours 3:1.
 * Every pair is a `warn`, so contrast never fails a deployment.
 */
export const CONTRAST: readonly ContrastPair[] = [
  {
    id: 'foreground-on-surfaces',
    text: '--color-foreground',
    on: SURFACE_NAMES,
    min: 4.5,
    severity: 'warn',
  },
  {
    id: 'muted-on-surfaces',
    text: '--color-muted',
    on: SURFACE_NAMES,
    min: 4.5,
    severity: 'warn',
  },
  {
    id: 'subtle-on-surfaces',
    text: '--color-subtle',
    on: SURFACE_NAMES,
    min: 4.5,
    severity: 'warn',
  },
  {
    id: 'accent-on-surfaces',
    text: '--color-accent',
    on: SURFACE_NAMES,
    min: 4.5,
    severity: 'warn',
  },
  {
    id: 'accent-foreground-on-accent',
    text: '--color-accent-foreground',
    on: ['--color-accent'],
    min: 4.5,
    severity: 'warn',
  },
  {
    id: 'status-degraded-on-surfaces',
    text: '--color-status-degraded',
    on: SURFACE_NAMES,
    min: 3,
    severity: 'warn',
  },
  {
    id: 'status-error-on-surfaces',
    text: '--color-status-error',
    on: SURFACE_NAMES,
    min: 3,
    severity: 'warn',
  },
  {
    id: 'status-healthy-on-surfaces',
    text: '--color-status-healthy',
    on: SURFACE_NAMES,
    min: 3,
    severity: 'warn',
  },
  {
    id: 'status-pending-on-surfaces',
    text: '--color-status-pending',
    on: SURFACE_NAMES,
    min: 3,
    severity: 'warn',
  },
];

export type SlotKind = 'image' | 'font';

export interface SlotDeclaration {
  readonly name: string;
  readonly kind: SlotKind;
  /** Extensions the slot's URL path may end in. */
  readonly formats: readonly string[];
  /** Modes the slot takes a value for; empty when one value serves both. */
  readonly modes: readonly Mode[];
}

/** Asset slots: the logo, the favicon and the two fonts. */
export const SLOTS: readonly SlotDeclaration[] = [
  { name: 'logo', kind: 'image', formats: ['svg'], modes: MODES },
  { name: 'favicon', kind: 'image', formats: ['svg'], modes: MODES },
  { name: 'font.sans', kind: 'font', formats: ['woff2'], modes: [] },
  { name: 'font.mono', kind: 'font', formats: ['woff2'], modes: [] },
];

export interface ProductNameBounds {
  /** Length limits apply after trimming surrounding whitespace. */
  readonly minLength: number;
  readonly maxLength: number;
  /** A Unicode-aware pattern (`u` flag) the whole trimmed string must match. */
  readonly pattern: string;
}

/** 1 to 40 characters after trimming: letters, digits, spaces and `.-&'`. */
export const PRODUCT_NAME: ProductNameBounds = {
  minLength: 1,
  maxLength: 40,
  pattern: "^[\\p{L}\\p{N} .\\-&']+$",
};
