/**
 * Builds the theme surface manifest from the token sheet and the surface
 * declaration. Pure: takes the CSS text and the declaration, returns the
 * manifest or throws naming every token missing from one side.
 */
import {
  FORMAT_FOR_KIND,
  MODES,
  PRODUCT_NAME,
  SLOTS,
  TOKEN_KINDS,
  type Mode,
  type ProductNameBounds,
  type SlotKind,
  type TokenFormat,
  type TokenKind,
} from '../../src/tokens/surface.ts';
import { RAW_PREFIX, isDirectReference, readTokenSheet, varReferences } from './css.ts';

export interface ManifestToken {
  readonly name: string;
  readonly kind: TokenKind;
  readonly format: TokenFormat;
  readonly modes: readonly Mode[];
}

export interface ManifestMapping {
  /** Raw tokens the semantic name resolves to, transitively, sorted. */
  readonly tokens: readonly string[];
  /** True for an expression such as `color-mix()` or `calc()`; false for a direct `var()` or a literal. */
  readonly derived: boolean;
}

export interface ManifestSlot {
  readonly name: string;
  readonly kind: SlotKind;
  readonly formats: readonly string[];
  readonly modes: readonly Mode[];
}

/**
 * The surface a theme can set. It carries no version: it ships inside a kora
 * release, and the release is its version.
 */
export interface Manifest {
  readonly modes: readonly Mode[];
  readonly tokens: readonly ManifestToken[];
  readonly mapping: Readonly<Record<string, ManifestMapping>>;
  readonly slots: readonly ManifestSlot[];
  readonly productName: ProductNameBounds;
}

export interface SurfaceInput {
  readonly tokenKinds: Readonly<Record<string, TokenKind>>;
  readonly slots: readonly ManifestSlot[];
  readonly productName: ProductNameBounds;
}

export const SURFACE: SurfaceInput = {
  tokenKinds: TOKEN_KINDS,
  slots: SLOTS,
  productName: PRODUCT_NAME,
};

export class SurfaceMismatchError extends Error {
  readonly missingFromSurface: readonly string[];
  readonly missingFromSheet: readonly string[];

  constructor(missingFromSurface: readonly string[], missingFromSheet: readonly string[]) {
    super(
      [
        'tokens.css and surface.ts disagree about the raw tokens:',
        ...missingFromSurface.map((t) => `  ${t} is in tokens.css but has no kind in surface.ts`),
        ...missingFromSheet.map(
          (t) => `  ${t} has a kind in surface.ts but is not declared in tokens.css`,
        ),
      ].join('\n'),
    );
    this.name = 'SurfaceMismatchError';
    this.missingFromSurface = missingFromSurface;
    this.missingFromSheet = missingFromSheet;
  }
}

export function buildManifest(css: string, surface: SurfaceInput = SURFACE): Manifest {
  const sheet = readTokenSheet(css);
  const declared = new Set(sheet.rawTokens);
  const known = new Set(Object.keys(surface.tokenKinds));
  const missingFromSurface = [...declared].filter((t) => !known.has(t)).sort();
  const missingFromSheet = [...known].filter((t) => !declared.has(t)).sort();
  if (missingFromSurface.length > 0 || missingFromSheet.length > 0) {
    throw new SurfaceMismatchError(missingFromSurface, missingFromSheet);
  }

  const tokens: ManifestToken[] = [...declared].sort().map((name) => {
    const kind = surface.tokenKinds[name]!;
    return { name, kind, format: FORMAT_FOR_KIND[kind], modes: MODES };
  });

  const semanticValues = new Map(sheet.semantic.map((d) => [d.name, d.value]));
  const resolve = (name: string, seen: Set<string>): string[] => {
    if (name.startsWith(RAW_PREFIX)) return declared.has(name) ? [name] : [];
    const value = semanticValues.get(name);
    if (value === undefined || seen.has(name)) return [];
    seen.add(name);
    return varReferences(value).flatMap((ref) => resolve(ref, seen));
  };
  const mapping: Record<string, ManifestMapping> = {};
  for (const { name, value } of [...sheet.semantic].sort((a, b) => a.name.localeCompare(b.name))) {
    const refs = varReferences(value);
    const resolved = [...new Set(refs.flatMap((ref) => resolve(ref, new Set())))].sort();
    const direct = isDirectReference(value);
    mapping[name] = {
      tokens: resolved,
      derived: !direct && refs.length > 0 && resolved.length > 0,
    };
  }

  const slots = [...surface.slots]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((s) => ({
      name: s.name,
      kind: s.kind,
      formats: [...s.formats],
      modes: [...s.modes],
    }));

  return {
    modes: MODES,
    tokens,
    mapping,
    slots,
    productName: surface.productName,
  };
}

/** Stable JSON with a trailing newline, as checked in. */
export function serializeManifest(manifest: Manifest): string {
  return `${JSON.stringify(manifest, null, 2)}\n`;
}

export function parseManifest(text: string): Manifest {
  const parsed: unknown = JSON.parse(text);
  if (typeof parsed !== 'object' || parsed === null) throw new Error('manifest is not an object');
  const m = parsed as Partial<Manifest>;
  for (const key of ['modes', 'tokens', 'mapping', 'slots', 'productName'] as const) {
    if (!(key in m)) throw new Error(`manifest is missing "${key}"`);
  }
  return m as Manifest;
}
