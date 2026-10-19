/** Builds the manifest from the CSS text and the surface declaration. Pure. */
import {
  FORMAT_FOR_KIND,
  CONTRAST,
  GROUPS,
  MODES,
  PRODUCT_NAME,
  SEMANTIC,
  SLOTS,
  TOKENS,
  type ContrastPair,
  type ContrastSeverity,
  type GroupDeclaration,
  type Mode,
  type Placement,
  type ProductNameBounds,
  type SlotKind,
  type TokenFormat,
  type TokenDeclaration,
  type TokenKind,
} from '../../src/tokens/surface.ts';
import {
  RAW_PREFIX,
  isDirectReference,
  parseBlocks,
  readTokenSheet,
  varReferences,
} from './css.ts';

export interface ManifestToken {
  readonly name: string;
  readonly kind: TokenKind;
  /** A `groups` id. */
  readonly group: string;
  /** Short display name within the group. */
  readonly label: string;
  readonly format: TokenFormat;
  readonly modes: readonly Mode[];
}

export interface ManifestGroup {
  /** Public surface: consumers hard-code it. */
  readonly id: string;
  readonly label: string;
  readonly order: number;
}

export interface ManifestMapping {
  /** A `groups` id. */
  readonly group: string;
  /** Short display name within the group. */
  readonly label: string;
  /** Raw tokens the semantic name resolves to, transitively, sorted. */
  readonly tokens: readonly string[];
  /** True for an expression such as `color-mix()`; false for a plain `var()` or a literal. */
  readonly derived: boolean;
}

export interface ManifestContrast {
  /** Consumers key on it. */
  readonly id: string;
  /** A non-derived colour semantic name: the text colour. */
  readonly text: string;
  /** Non-derived colour semantic names the text sits on. */
  readonly on: readonly string[];
  /** WCAG 2.x contrast ratio the text must reach on each `on` colour. */
  readonly min: number;
  readonly severity: ContrastSeverity;
}

export interface ManifestSlot {
  readonly name: string;
  readonly kind: SlotKind;
  readonly formats: readonly string[];
  readonly modes: readonly Mode[];
}

/** The surface a theme can set. It has no version field; the kora release is its version. */
export interface Manifest {
  readonly modes: readonly Mode[];
  /** Flat, in display order; every token and mapping entry names one. */
  readonly groups: readonly ManifestGroup[];
  readonly tokens: readonly ManifestToken[];
  readonly mapping: Readonly<Record<string, ManifestMapping>>;
  /** Text pairs a theme must keep, sorted by id. Empty in a manifest from before contrast existed. */
  readonly contrast: readonly ManifestContrast[];
  readonly slots: readonly ManifestSlot[];
  readonly productName: ProductNameBounds;
}

export interface SurfaceInput {
  readonly tokens: Readonly<Record<string, TokenDeclaration>>;
  readonly semantic: Readonly<Record<string, Placement>>;
  readonly groups: readonly GroupDeclaration[];
  readonly contrast: readonly ContrastPair[];
  readonly slots: readonly ManifestSlot[];
  readonly productName: ProductNameBounds;
}

export const SURFACE: SurfaceInput = {
  tokens: TOKENS,
  semantic: SEMANTIC,
  groups: GROUPS,
  contrast: CONTRAST,
  slots: SLOTS,
  productName: PRODUCT_NAME,
};

export class SurfaceMismatchError extends Error {
  readonly problems: readonly string[];

  constructor(problems: readonly string[]) {
    super(
      [
        'tokens.css and surface.ts disagree, or surface.ts declares a bad group or contrast pair:',
        ...problems,
      ].join('\n'),
    );
    this.name = 'SurfaceMismatchError';
    this.problems = problems;
  }
}

/**
 * Every fault between the token sheet and the declaration: a token or semantic
 * name on one side only, a placement with no group or an unknown one, a group
 * declared twice, out of order or with no member.
 */
function surfaceProblems(
  declaredRaw: ReadonlySet<string>,
  declaredSemantic: ReadonlySet<string>,
  surface: SurfaceInput,
): string[] {
  const problems: string[] = [];
  const diff = (
    sheet: ReadonlySet<string>,
    declared: Readonly<Record<string, unknown>>,
    noun: string,
    missing: string,
  ) => {
    for (const t of [...sheet].filter((n) => !(n in declared)).sort())
      problems.push(`  ${t} is in tokens.css but has no ${missing} in surface.ts`);
    for (const t of Object.keys(declared)
      .filter((n) => !sheet.has(n))
      .sort())
      problems.push(`  ${t} has a ${noun} in surface.ts but is not declared in tokens.css`);
  };
  diff(declaredRaw, surface.tokens, 'kind', 'kind');
  diff(declaredSemantic, surface.semantic, 'group', 'group');

  const ids = new Set<string>();
  const orders = new Set<number>();
  for (const g of surface.groups) {
    if (g.id === '' || g.label === '') problems.push(`  group "${g.id}" needs an id and a label`);
    if (ids.has(g.id)) problems.push(`  group "${g.id}" is declared twice`);
    if (orders.has(g.order)) problems.push(`  group "${g.id}" repeats order ${g.order}`);
    ids.add(g.id);
    orders.add(g.order);
  }

  const used = new Set<string>();
  const place = (name: string, p: Partial<Placement> | undefined) => {
    if (p === undefined) return;
    if (p.group === undefined || p.group === '') {
      problems.push(`  ${name} declares no group in surface.ts`);
    } else if (!ids.has(p.group)) {
      problems.push(`  ${name} declares unknown group "${p.group}" in surface.ts`);
    } else {
      used.add(p.group);
    }
    if (p.label === undefined || p.label === '')
      problems.push(`  ${name} declares no label in surface.ts`);
  };
  for (const [name, d] of Object.entries(surface.tokens)) place(name, d);
  for (const [name, p] of Object.entries(surface.semantic)) place(name, p);
  for (const id of ids)
    if (!used.has(id)) problems.push(`  group "${id}" has no token or semantic name`);
  return problems;
}

const MAX_CONTRAST = 21;

/**
 * Every fault in the contrast pairs: an empty or repeated id, a name that is
 * unknown, not a colour or derived (accent-subtle and the like have no single
 * raw colour to measure), an empty or repeated `on`, a text listed under its
 * own `on`, a minimum outside [1, 21] and a severity that is not `error` or
 * `warn`.
 */
function contrastProblems(
  pairs: readonly ContrastPair[],
  surface: SurfaceInput,
  derived: ReadonlyMap<string, boolean>,
  tokenNames: ReadonlyMap<string, readonly string[]>,
): string[] {
  const problems: string[] = [];
  const ids = new Set<string>();
  const nameFault = (id: string, role: string, name: string): void => {
    const tokens = tokenNames.get(name);
    if (tokens === undefined) {
      problems.push(`  contrast pair "${id}" ${role} ${name}, which is not a semantic name`);
    } else if (derived.get(name)) {
      problems.push(
        `  contrast pair "${id}" ${role} ${name}, which is derived and has no single colour`,
      );
    } else if (tokens.length !== 1 || surface.tokens[tokens[0]!]?.kind !== 'color') {
      problems.push(`  contrast pair "${id}" ${role} ${name}, which is not a colour`);
    }
  };
  for (const p of pairs) {
    if (p.id === '') problems.push('  a contrast pair has an empty id');
    else if (ids.has(p.id)) problems.push(`  contrast pair "${p.id}" is declared twice`);
    ids.add(p.id);
    nameFault(p.id, 'text', p.text);
    if (p.on.length === 0) problems.push(`  contrast pair "${p.id}" has an empty "on"`);
    const seen = new Set<string>();
    for (const name of p.on) {
      if (seen.has(name)) problems.push(`  contrast pair "${p.id}" lists ${name} twice in "on"`);
      seen.add(name);
      if (name === p.text)
        problems.push(`  contrast pair "${p.id}" lists its own text ${name} in "on"`);
      nameFault(p.id, 'is on', name);
    }
    if (!Number.isFinite(p.min) || p.min < 1 || p.min > MAX_CONTRAST)
      problems.push(
        `  contrast pair "${p.id}" has min ${p.min}; it must be between 1 and ${MAX_CONTRAST}`,
      );
    if ((p.severity as string) !== 'error' && (p.severity as string) !== 'warn')
      problems.push(
        `  contrast pair "${p.id}" has severity "${p.severity as string}"; use error or warn`,
      );
  }
  return problems;
}

export function buildManifest(css: string, surface: SurfaceInput = SURFACE): Manifest {
  const sheet = readTokenSheet(css);
  const declared = new Set(sheet.rawTokens);
  const problems = surfaceProblems(declared, new Set(sheet.semantic.map((d) => d.name)), surface);
  if (problems.length > 0) throw new SurfaceMismatchError(problems);

  const tokens: ManifestToken[] = [...declared].sort().map((name) => {
    const { kind, group, label } = surface.tokens[name]!;
    return { name, kind, group, label, format: FORMAT_FOR_KIND[kind], modes: MODES };
  });

  const semanticValues = new Map(sheet.semantic.map((d) => [d.name, d.value]));
  const resolve = (name: string, seen: Set<string>): string[] => {
    if (name.startsWith(RAW_PREFIX)) return declared.has(name) ? [name] : [];
    const value = semanticValues.get(name);
    if (value === undefined || seen.has(name)) return [];
    seen.add(name);
    return varReferences(value).flatMap((ref) => resolve(ref, seen));
  };
  // Derived means the colour is computed somewhere along the chain, so an alias
  // of a derived name is derived too.
  const isDerived = (name: string, seen: Set<string>): boolean => {
    const value = semanticValues.get(name);
    if (value === undefined || seen.has(name)) return false;
    seen.add(name);
    const refs = varReferences(value);
    const resolved = refs.flatMap((ref) => resolve(ref, new Set()));
    if (!isDirectReference(value) && refs.length > 0 && resolved.length > 0) return true;
    return refs.some((ref) => !ref.startsWith(RAW_PREFIX) && isDerived(ref, seen));
  };
  const mapping: Record<string, ManifestMapping> = {};
  for (const { name, value } of [...sheet.semantic].sort((a, b) => a.name.localeCompare(b.name))) {
    const refs = varReferences(value);
    const resolved = [...new Set(refs.flatMap((ref) => resolve(ref, new Set())))].sort();
    const { group, label } = surface.semantic[name]!;
    mapping[name] = {
      group,
      label,
      tokens: resolved,
      derived: isDerived(name, new Set()),
    };
  }

  const contrastFaults = contrastProblems(
    surface.contrast,
    surface,
    new Map(Object.entries(mapping).map(([n, m]) => [n, m.derived])),
    new Map(Object.entries(mapping).map(([n, m]) => [n, m.tokens])),
  );
  if (contrastFaults.length > 0) throw new SurfaceMismatchError(contrastFaults);

  const contrast = [...surface.contrast]
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
    .map((p) => ({ id: p.id, text: p.text, on: [...p.on], min: p.min, severity: p.severity }));

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
    groups: [...surface.groups]
      .sort((a, b) => a.order - b.order)
      .map(({ id, label, order }) => ({ id, label, order })),
    tokens,
    mapping,
    contrast,
    slots,
    productName: surface.productName,
  };
}

/** Raw colour token to value, per mode. */
export type TokenDefaults = Readonly<Record<Mode, Readonly<Record<string, string>>>>;

/**
 * The raw colour values `tokens.css` ships, per mode: `:root` for dark, then
 * the `[data-theme='light']` overrides on top of it for light. Written to
 * `defaults.json`, apart from the manifest: default values are not part of
 * the theme surface, and the validator reads them only to check a theme's
 * contrast pairs against the colours the theme leaves alone.
 */
export function buildTokenDefaults(css: string, surface: SurfaceInput = SURFACE): TokenDefaults {
  const dark = new Map<string, string>();
  const light = new Map<string, string>();
  for (const block of parseBlocks(css)) {
    const target =
      block.selector === ':root' ? dark : block.selector === "[data-theme='light']" ? light : null;
    if (!target) continue;
    for (const d of block.declarations)
      if (surface.tokens[d.name]?.kind === 'color') target.set(d.name, d.value);
  }
  const sorted = (m: Map<string, string>) =>
    Object.fromEntries([...m].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
  return { dark: sorted(dark), light: sorted(new Map([...dark, ...light])) };
}

/** Stable JSON with a trailing newline, as checked in. */
export function serializeManifest(manifest: Manifest): string {
  return `${JSON.stringify(manifest, null, 2)}\n`;
}

export function serializeDefaults(defaults: TokenDefaults): string {
  return `${JSON.stringify(defaults, null, 2)}\n`;
}
