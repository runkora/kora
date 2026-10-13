/**
 * The deterministic classifier: compares the manifest, the component API and
 * the changed files at head against those at the baseline and decides, on two
 * axes, what the change does to the package version (patch, minor or major,
 * Semantic Versioning) and to the contract version (none, minor or major),
 * citing the rule for every finding. Pure; `scripts/classify.ts` supplies git
 * and the filesystem. See the design system versioning ADR, SD-3.
 */
import type { Manifest, ManifestSlot, ManifestToken } from './manifest.ts';
import type { ExportSurface } from './exports.ts';
import { compareRanges } from './ranges.ts';

export type Level = 'none' | 'patch' | 'minor' | 'major';
/** The contract version has no patch: a change that leaves the surface alone does not move it. */
export type ContractLevel = 'none' | 'minor' | 'major';

const ORDER: Record<Level, number> = { none: 0, patch: 1, minor: 2, major: 3 };

export function maxLevel(a: Level, b: Level): Level {
  return ORDER[a] >= ORDER[b] ? a : b;
}

export const RULE_1 = 'boundary rule 1';
export const RULE_4 = 'boundary rule 4';
export const RULE_5 = 'boundary rule 5';
export const SEMVER = 'Semantic Versioning';

export interface Finding {
  /** What the finding does to the contract version. */
  readonly contract: ContractLevel;
  /** What the finding does to the package version; never below `contract`. */
  readonly package: Level;
  readonly rule: string;
  /** The token, slot, semantic name, export or file set concerned. */
  readonly subject: string;
  readonly detail: string;
  /** Source lines that decided a read-by-existing-component finding. */
  readonly matches?: readonly string[];
}

/**
 * The manifest, `tokens.css` and the schema are part of the package's public
 * surface, so a contract change is always a package change of at least the
 * same level. The reverse does not hold: a removed prop moves the package and
 * leaves the contract alone.
 */
function finding(
  contract: ContractLevel,
  pkg: Level,
  rule: string,
  subject: string,
  detail: string,
  matches?: readonly string[],
): Finding {
  return {
    contract,
    package: maxLevel(pkg, contract),
    rule,
    subject,
    detail,
    ...(matches ? { matches } : {}),
  };
}

export interface ClassifyInput {
  readonly baseManifest: Manifest | null;
  readonly headManifest: Manifest;
  readonly baseExports: ExportSurface | null;
  readonly headExports: ExportSurface;
  readonly basePeerDependencies: Readonly<Record<string, string>>;
  readonly headPeerDependencies: Readonly<Record<string, string>>;
  /** Package-relative paths changed between baseline and head. */
  readonly changedFiles: readonly string[];
  /**
   * Searches the component sources that existed at the baseline, as they are at
   * head, for any of the patterns; returns `file:line: text` for each hit.
   */
  readonly findReads: (patterns: readonly RegExp[]) => readonly string[];
}

export interface Classification {
  readonly package: Level;
  readonly contract: ContractLevel;
  readonly findings: readonly Finding[];
}

/** Files whose change alone classifies as none. */
export function isDocumentationOnly(relPath: string): boolean {
  return (
    relPath === 'README.md' ||
    relPath === 'CONTRACT.md' ||
    relPath.startsWith('src/docs/') ||
    relPath.startsWith('.storybook/') ||
    relPath === 'src/test-setup.ts' ||
    /\.stories\.tsx?$/.test(relPath) ||
    /\.test\.tsx?$/.test(relPath)
  );
}

export function classify(input: ClassifyInput): Classification {
  const findings: Finding[] = [];
  if (input.baseManifest)
    findings.push(...compareManifests(input.baseManifest, input.headManifest, input.findReads));
  if (input.baseExports) findings.push(...compareExports(input.baseExports, input.headExports));
  findings.push(...comparePeerDependencies(input.basePeerDependencies, input.headPeerDependencies));

  const sourceChanges = input.changedFiles.filter((f) => !isDocumentationOnly(f));
  if (sourceChanges.length > 0) {
    findings.push(
      finding(
        'none',
        'patch',
        RULE_5,
        `${sourceChanges.length} source file${sourceChanges.length === 1 ? '' : 's'}`,
        `changed without changing the contract: ${sourceChanges.slice(0, 5).join(', ')}${sourceChanges.length > 5 ? ', …' : ''}`,
      ),
    );
  }

  const pkg = findings.reduce<Level>((acc, f) => maxLevel(acc, f.package), 'none');
  const contract = findings.reduce<ContractLevel>(
    (acc, f) => (ORDER[f.contract] > ORDER[acc] ? f.contract : acc),
    'none',
  );
  return {
    package: pkg,
    contract,
    findings: [...findings].sort(
      (a, b) => ORDER[b.package] - ORDER[a.package] || ORDER[b.contract] - ORDER[a.contract],
    ),
  };
}

function compareManifests(
  base: Manifest,
  head: Manifest,
  findReads: ClassifyInput['findReads'],
): Finding[] {
  const out: Finding[] = [];

  if (!sameList(base.modes, head.modes)) {
    out.push(
      finding('major', 'major', RULE_1, 'modes', `${fmt(base.modes)} became ${fmt(head.modes)}`),
    );
  }

  const baseTokens = new Map(base.tokens.map((t) => [t.name, t]));
  const headTokens = new Map(head.tokens.map((t) => [t.name, t]));
  for (const [name, b] of baseTokens) {
    const h = headTokens.get(name);
    if (!h) {
      out.push(finding('major', 'major', RULE_1, name, 'token removed or renamed'));
      continue;
    }
    out.push(...tokenFieldChanges(name, b, h));
  }
  for (const [name] of headTokens) {
    if (baseTokens.has(name)) continue;
    const semanticNames = Object.entries(head.mapping)
      .filter(([, m]) => m.tokens.includes(name))
      .map(([s]) => s);
    const patterns = [tokenPattern(name), ...semanticNames.flatMap(semanticPatterns)];
    const matches = findReads(patterns);
    out.push(
      matches.length > 0
        ? finding(
            'major',
            'major',
            RULE_4,
            name,
            'token added and read by a component that existed at the baseline',
            matches,
          )
        : finding(
            'minor',
            'minor',
            RULE_4,
            name,
            'token added; read only by components new since the baseline',
          ),
    );
  }

  for (const [name, b] of Object.entries(base.mapping)) {
    const h = head.mapping[name];
    if (!h) {
      // No theme sets a semantic name, but a consumer's styles may read it.
      out.push(finding('none', 'major', SEMVER, name, 'semantic name removed'));
      continue;
    }
    if (!sameList(b.tokens, h.tokens)) {
      out.push(
        finding(
          'major',
          'major',
          RULE_4,
          name,
          `semantic name remapped from ${fmt(b.tokens)} to ${fmt(h.tokens)}`,
        ),
      );
    }
  }
  for (const name of Object.keys(head.mapping)) {
    if (!(name in base.mapping)) {
      out.push(finding('none', 'minor', SEMVER, name, 'semantic name added over existing tokens'));
    }
  }

  const baseSlots = new Map(base.slots.map((s) => [s.name, s]));
  const headSlots = new Map(head.slots.map((s) => [s.name, s]));
  for (const [name, b] of baseSlots) {
    const h = headSlots.get(name);
    if (!h) {
      out.push(finding('major', 'major', RULE_1, name, 'slot removed or renamed'));
      continue;
    }
    out.push(...slotFieldChanges(name, b, h));
  }
  for (const [name] of headSlots) {
    if (baseSlots.has(name)) continue;
    const matches = findReads([slotPattern(name)]);
    out.push(
      matches.length > 0
        ? finding(
            'major',
            'major',
            RULE_4,
            name,
            'slot added and read by a component that existed at the baseline',
            matches,
          )
        : finding(
            'minor',
            'minor',
            RULE_4,
            name,
            'slot added; read only by components new since the baseline',
          ),
    );
  }

  const pb = base.productName;
  const ph = head.productName;
  if (ph.minLength > pb.minLength || ph.maxLength < pb.maxLength || ph.pattern !== pb.pattern) {
    out.push(
      finding('major', 'major', RULE_1, 'productName', 'bounds tightened or pattern changed'),
    );
  } else if (ph.minLength < pb.minLength || ph.maxLength > pb.maxLength) {
    out.push(finding('minor', 'minor', RULE_1, 'productName', 'bounds loosened'));
  }

  return out;
}

function tokenFieldChanges(name: string, b: ManifestToken, h: ManifestToken): Finding[] {
  const out: Finding[] = [];
  if (b.kind !== h.kind)
    out.push(finding('major', 'major', RULE_1, name, `kind ${b.kind} became ${h.kind}`));
  if (b.format !== h.format)
    out.push(finding('major', 'major', RULE_1, name, `format ${b.format} became ${h.format}`));
  if (!sameList(b.modes, h.modes))
    out.push(
      finding('major', 'major', RULE_1, name, `modes ${fmt(b.modes)} became ${fmt(h.modes)}`),
    );
  return out;
}

function slotFieldChanges(name: string, b: ManifestSlot, h: ManifestSlot): Finding[] {
  const out: Finding[] = [];
  if (b.kind !== h.kind)
    out.push(finding('major', 'major', RULE_1, name, `kind ${b.kind} became ${h.kind}`));
  if (!sameList(b.modes, h.modes))
    out.push(
      finding('major', 'major', RULE_1, name, `modes ${fmt(b.modes)} became ${fmt(h.modes)}`),
    );
  const dropped = b.formats.filter((f) => !h.formats.includes(f));
  const gained = h.formats.filter((f) => !b.formats.includes(f));
  if (dropped.length > 0)
    out.push(finding('major', 'major', RULE_1, name, `formats no longer accept ${fmt(dropped)}`));
  else if (gained.length > 0)
    out.push(finding('minor', 'minor', RULE_1, name, `formats now also accept ${fmt(gained)}`));
  if (h.maxBytes < b.maxBytes)
    out.push(
      finding(
        'major',
        'major',
        RULE_1,
        name,
        `maximum size shrank from ${b.maxBytes} to ${h.maxBytes} bytes`,
      ),
    );
  else if (h.maxBytes > b.maxBytes)
    out.push(
      finding(
        'minor',
        'minor',
        RULE_1,
        name,
        `maximum size grew from ${b.maxBytes} to ${h.maxBytes} bytes`,
      ),
    );
  return out;
}

/**
 * The component API rows. None of these touch the contract: no theme sets an
 * export or a prop. They are Semantic Versioning for the package.
 */
function compareExports(base: ExportSurface, head: ExportSurface): Finding[] {
  const out: Finding[] = [];
  const baseNames = new Set(base.exports);
  const headNames = new Set(head.exports);
  for (const name of base.exports) {
    if (!headNames.has(name))
      out.push(finding('none', 'major', SEMVER, name, 'export removed or renamed'));
  }
  for (const name of head.exports) {
    if (!baseNames.has(name)) out.push(finding('none', 'minor', SEMVER, name, 'export added'));
  }
  for (const [type, members] of Object.entries(base.props)) {
    const now = head.props[type];
    if (!now || !headNames.has(type)) continue;
    const removed = members.filter((m) => !now.includes(m));
    if (removed.length > 0)
      out.push(
        finding(
          'none',
          'major',
          SEMVER,
          type,
          `prop${removed.length === 1 ? '' : 's'} removed: ${removed.join(', ')}`,
        ),
      );
    const baseRequired = new Set(base.required[type] ?? []);
    const headRequired = new Set(head.required[type] ?? []);
    const added = now.filter((m) => !members.includes(m));
    const addedRequired = added.filter((m) => headRequired.has(m));
    const addedOptional = added.filter((m) => !headRequired.has(m));
    const nowRequired = now.filter(
      (m) => members.includes(m) && headRequired.has(m) && !baseRequired.has(m),
    );
    if (addedRequired.length > 0)
      out.push(
        finding(
          'none',
          'major',
          SEMVER,
          type,
          `required prop${addedRequired.length === 1 ? '' : 's'} added: ${addedRequired.join(', ')}`,
        ),
      );
    if (nowRequired.length > 0)
      out.push(
        finding(
          'none',
          'major',
          SEMVER,
          type,
          `prop${nowRequired.length === 1 ? '' : 's'} made required: ${nowRequired.join(', ')}`,
        ),
      );
    if (addedOptional.length > 0)
      out.push(
        finding(
          'none',
          'minor',
          SEMVER,
          type,
          `optional prop${addedOptional.length === 1 ? '' : 's'} added: ${addedOptional.join(', ')}`,
        ),
      );
  }
  return out;
}

/**
 * A peer dependency added, or its range narrowed, obliges every consumer to
 * change: major. A range widened, or a peer removed, which accepts every
 * version the old constraint rejected, is minor. Without a baseline set of
 * peers there is nothing to add to.
 */
function comparePeerDependencies(
  base: Readonly<Record<string, string>>,
  head: Readonly<Record<string, string>>,
): Finding[] {
  const out: Finding[] = [];
  for (const [name, range] of Object.entries(head)) {
    const before = base[name];
    if (before === undefined) {
      if (Object.keys(base).length > 0)
        out.push(finding('none', 'major', SEMVER, name, `peer dependency added (${range})`));
      continue;
    }
    const change = compareRanges(before, range);
    if (change === 'narrowed')
      out.push(
        finding(
          'none',
          'major',
          SEMVER,
          name,
          `peer dependency range narrowed from ${before} to ${range}`,
        ),
      );
    else if (change === 'widened')
      out.push(
        finding(
          'none',
          'minor',
          SEMVER,
          name,
          `peer dependency range widened from ${before} to ${range}`,
        ),
      );
  }
  for (const [name, range] of Object.entries(base)) {
    if (!(name in head))
      out.push(finding('none', 'minor', SEMVER, name, `peer dependency removed (was ${range})`));
  }
  return out;
}

/** A direct `var(--hades-raw-x)` read of a raw token. */
export function tokenPattern(rawName: string): RegExp {
  return new RegExp(`var\\(\\s*${escape(rawName)}\\s*[,)]`);
}

/**
 * Reads of a semantic name: its `var()` form, and the Tailwind utilities it
 * generates (`bg-accent`, `hover:text-accent/80`, `shadow-elevated`,
 * `font-mono`, `rounded-md`). The trailing guard stops `accent` matching
 * `accent-subtle`.
 */
export function semanticPatterns(semanticName: string): RegExp[] {
  const m = /^--(color|shadow|font|radius|ease)-(.+)$/.exec(semanticName);
  const patterns = [new RegExp(`var\\(\\s*${escape(semanticName)}\\s*[,)]`)];
  if (!m) return patterns;
  const suffix = escape(m[2]!);
  const prefixes: Record<string, string> = {
    color:
      '(?:bg|text|border|ring|fill|stroke|from|to|via|outline|decoration|caret|divide|placeholder|accent|shadow|inset-ring)(?:-[xytblrse])?',
    shadow: 'shadow',
    font: 'font',
    radius: 'rounded(?:-(?:t|b|l|r|s|e|tl|tr|bl|br|ss|se|es|ee))?',
    ease: 'ease',
  };
  patterns.push(new RegExp(`(?:^|[^\\w-])(?:[\\w-]+:)*${prefixes[m[1]!]}-${suffix}(?![\\w-])`));
  return patterns;
}

/** A slot is read by its quoted name. */
export function slotPattern(slotName: string): RegExp {
  return new RegExp(`['"\`]${escape(slotName)}['"\`]`);
}

function escape(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function sameList(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((x, i) => x === b[i]);
}

function fmt(list: readonly string[]): string {
  return list.length === 0 ? '(none)' : list.join(', ');
}

// ─── Versions and the floor ──────────────────────────────────────────────────

export interface Semver {
  readonly major: number;
  readonly minor: number;
  readonly patch: number;
}

export function parseSemver(text: string): Semver | null {
  const m = /^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$/.exec(text.trim());
  return m ? { major: Number(m[1]), minor: Number(m[2]), patch: Number(m[3]) } : null;
}

export function compareSemver(a: Semver, b: Semver): number {
  return a.major - b.major || a.minor - b.minor || a.patch - b.patch;
}

export function formatSemver(v: Semver): string {
  return `${v.major}.${v.minor}.${v.patch}`;
}

/** The lowest version that satisfies the floor for `level` above `base`. */
export function requiredVersion(level: Level, base: Semver): Semver {
  switch (level) {
    case 'major':
      return { major: base.major + 1, minor: 0, patch: 0 };
    case 'minor':
      return { major: base.major, minor: base.minor + 1, patch: 0 };
    case 'patch':
      return { major: base.major, minor: base.minor, patch: base.patch + 1 };
    case 'none':
      return base;
  }
}

export function floorMet(level: Level, base: Semver, head: Semver): boolean {
  if (level === 'none') return compareSemver(head, base) >= 0;
  const required = requiredVersion(level, base);
  if (level === 'major') return head.major >= required.major;
  if (level === 'minor')
    return head.major > base.major || (head.major === base.major && head.minor >= required.minor);
  return compareSemver(head, required) >= 0;
}

/** The contract version: `MAJOR.MINOR`, no patch. */
export interface ContractVersion {
  readonly major: number;
  readonly minor: number;
}

export function parseContractVersion(text: string): ContractVersion | null {
  const m = /^(\d+)\.(\d+)$/.exec(text.trim());
  return m ? { major: Number(m[1]), minor: Number(m[2]) } : null;
}

export function formatContractVersion(v: ContractVersion): string {
  return `${v.major}.${v.minor}`;
}

export function compareContractVersions(a: ContractVersion, b: ContractVersion): number {
  return a.major - b.major || a.minor - b.minor;
}

/** The lowest contract version that satisfies the floor for `level` above `base`; a major resets the minor. */
export function requiredContractVersion(
  level: ContractLevel,
  base: ContractVersion,
): ContractVersion {
  switch (level) {
    case 'major':
      return { major: base.major + 1, minor: 0 };
    case 'minor':
      return { major: base.major, minor: base.minor + 1 };
    case 'none':
      return base;
  }
}

export function contractFloorMet(
  level: ContractLevel,
  base: ContractVersion,
  head: ContractVersion,
): boolean {
  if (level === 'none') return compareContractVersions(head, base) >= 0;
  const required = requiredContractVersion(level, base);
  if (level === 'major') return head.major >= required.major;
  return head.major > base.major || (head.major === base.major && head.minor >= required.minor);
}
