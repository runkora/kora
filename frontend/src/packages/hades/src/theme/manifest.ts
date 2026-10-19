/**
 * The parts of `manifest.json` the theme module reads. A structural type
 * rather than an import from `scripts/`, because the scripts are not shipped
 * in the package and a consumer hands the module the manifest it imported.
 */
import type { Mode, ProductNameBounds, SlotKind, TokenFormat, TokenKind } from '../tokens/surface';

export interface ThemeManifestToken {
  readonly name: string;
  readonly kind: TokenKind;
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

export interface ThemeManifest {
  readonly modes: readonly Mode[];
  readonly tokens: readonly ThemeManifestToken[];
  readonly slots: readonly ThemeManifestSlot[];
  readonly productName: ProductNameBounds;
}

/**
 * The family token a font slot sets, by rule: `font.<name>` sets
 * `--hades-raw-font-<name>`. Null when the manifest has no such token.
 */
export function familyTokenForSlot(manifest: ThemeManifest, slotName: string): string | null {
  if (!slotName.startsWith('font.')) return null;
  const token = `--hades-raw-font-${slotName.slice('font.'.length)}`;
  return manifest.tokens.some((t) => t.name === token) ? token : null;
}

/** The family name the instance assigns to a font slot's face: `hades-theme-<name>`. */
export function familyNameForSlot(slotName: string): string {
  return `hades-theme-${slotName.slice('font.'.length)}`;
}
