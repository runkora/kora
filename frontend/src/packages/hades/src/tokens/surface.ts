/**
 * What a theme can set that `tokens.css` does not say: token kinds, slots and name bounds.
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

/** Kind of every raw token in `tokens.css`, keyed by property name. */
export const TOKEN_KINDS: Readonly<Record<string, TokenKind>> = {
  // Neutral scale
  '--hades-raw-black': 'color',
  '--hades-raw-950': 'color',
  '--hades-raw-900': 'color',
  '--hades-raw-800': 'color',
  '--hades-raw-700': 'color',
  '--hades-raw-600': 'color',
  '--hades-raw-500': 'color',
  '--hades-raw-400': 'color',
  '--hades-raw-300': 'color',
  '--hades-raw-200': 'color',
  '--hades-raw-100': 'color',
  '--hades-raw-white': 'color',

  // Accent scale
  '--hades-raw-accent-600': 'color',
  '--hades-raw-accent-500': 'color',
  '--hades-raw-accent-400': 'color',
  '--hades-raw-accent-300': 'color',
  '--hades-raw-accent-fg': 'color',

  // Status
  '--hades-raw-healthy': 'color',
  '--hades-raw-degraded': 'color',
  '--hades-raw-error': 'color',
  '--hades-raw-pending': 'color',

  // Provider and strategy identity
  '--hades-raw-provider-bedrock': 'color',
  '--hades-raw-provider-vertexai': 'color',
  '--hades-raw-provider-azure': 'color',
  '--hades-raw-strategy-weighted': 'color',

  // Chart series
  '--hades-raw-chart-1': 'color',
  '--hades-raw-chart-2': 'color',
  '--hades-raw-chart-3': 'color',
  '--hades-raw-chart-4': 'color',
  '--hades-raw-chart-5': 'color',
  '--hades-raw-chart-6': 'color',
  '--hades-raw-chart-7': 'color',
  '--hades-raw-chart-8': 'color',

  // Overlay
  '--hades-raw-overlay': 'color',
  '--hades-raw-overlay-strong': 'color',

  // Shadows
  '--hades-raw-shadow-sm': 'shadow',
  '--hades-raw-shadow-elevated': 'shadow',

  // Text and border minima
  '--hades-raw-subtle': 'color',
  '--hades-raw-border': 'color',
  '--hades-raw-border-subtle': 'color',

  // Radius scale
  '--hades-raw-radius-sm': 'length',
  '--hades-raw-radius-md': 'length',
  '--hades-raw-radius-lg': 'length',
  '--hades-raw-radius-xl': 'length',
  '--hades-raw-radius-2xl': 'length',

  // Font slots
  '--hades-raw-font-sans': 'font',
  '--hades-raw-font-mono': 'font',
};

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
