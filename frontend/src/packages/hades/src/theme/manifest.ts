/** The parts of `manifest.json` the theme module reads. Structural, because the package does not ship `scripts/`. */
import type { Mode, ProductNameBounds, SlotKind, TokenFormat, TokenKind } from '../tokens/surface';

export interface ThemeManifestToken {
  readonly name: string;
  readonly kind: TokenKind;
  /** A `groups` id. Display metadata, not contract. Absent in older manifests. */
  readonly group?: string;
  readonly label?: string;
  readonly format: TokenFormat;
  readonly modes: readonly Mode[];
}

export interface ThemeManifestSlot {
  readonly name: string;
  readonly kind: SlotKind;
  /** Extensions the slot's URL path may end in. */
  readonly formats: readonly string[];
  /** Empty when one URL serves both modes. */
  readonly modes: readonly Mode[];
}

export interface ThemeManifestMapping {
  /** Raw tokens the semantic name resolves to, transitively. */
  readonly tokens: readonly string[];
  /** True for an expression such as `color-mix()`: no single raw colour. */
  readonly derived: boolean;
}

export interface ThemeManifestContrast {
  readonly id: string;
  /** A non-derived colour semantic name. */
  readonly text: string;
  readonly on: readonly string[];
  /** WCAG 2.x contrast ratio the text must reach on each `on` colour. */
  readonly min: number;
  readonly severity: 'error' | 'warn';
}

export interface ThemeManifest {
  readonly modes: readonly Mode[];
  readonly tokens: readonly ThemeManifestToken[];
  /** Semantic name to its raw tokens. Absent in older manifests. */
  readonly mapping?: Readonly<Record<string, ThemeManifestMapping>>;
  /** Contrast pairs the theme must keep. Absent in older manifests, which then check none. */
  readonly contrast?: readonly ThemeManifestContrast[];
  readonly slots: readonly ThemeManifestSlot[];
  readonly productName: ProductNameBounds;
}

/** The family token a font slot sets (`font.<name>` sets `--hades-raw-font-<name>`), or null if the manifest lacks it. */
export function familyTokenForSlot(manifest: ThemeManifest, slotName: string): string | null {
  if (!slotName.startsWith('font.')) return null;
  const token = `--hades-raw-font-${slotName.slice('font.'.length)}`;
  return manifest.tokens.some((t) => t.name === token) ? token : null;
}

/** The family name the instance assigns to a font slot's face: `hades-theme-<name>`. */
export function familyNameForSlot(slotName: string): string {
  return `hades-theme-${slotName.slice('font.'.length)}`;
}
