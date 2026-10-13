/**
 * Compares two npm version ranges, for the peer dependency rows of the
 * versioning ADR's classification table: a new range that accepts every
 * version the old one did is widened, and any other change is narrowed.
 * Overlapping and touching alternatives are merged first, so a union is judged
 * by what it covers, not by how it is written.
 *
 * Handles the forms a peer range takes in practice: `*`, `>=`, `>`, `<=`,
 * `<`, `=`, bare and partial versions, `^`, `~`, whitespace-separated
 * comparators and `||`. Pre-release tags are ignored. A range it cannot parse
 * is narrowed, the direction that demands a major bump.
 */
type Version = readonly [number, number, number];

interface Interval {
  readonly lo: Version | null;
  readonly loInclusive: boolean;
  readonly hi: Version | null;
  readonly hiInclusive: boolean;
}

export type RangeChange = 'same' | 'widened' | 'narrowed';

const ANY: Interval = { lo: null, loInclusive: true, hi: null, hiInclusive: true };

export function compareRanges(before: string, after: string): RangeChange {
  if (before.trim() === after.trim()) return 'same';
  try {
    const old = parseRange(before);
    const now = parseRange(after);
    const widens = subset(old, now);
    const narrows = subset(now, old);
    if (widens && narrows) return 'same';
    return widens ? 'widened' : 'narrowed';
  } catch {
    return 'narrowed';
  }
}

function parseRange(text: string): Interval[] {
  const alternatives = text
    .split('||')
    .map((alt) => alt.trim())
    .map((alt) =>
      alt
        .split(/\s+/)
        .filter(Boolean)
        .reduce(intersectWith, ANY as Interval | null),
    );
  return normalize(alternatives.filter((i): i is Interval => i !== null && !isEmpty(i)));
}

function intersectWith(acc: Interval | null, comparator: string): Interval | null {
  return acc === null ? null : intersect(acc, parseComparator(comparator));
}

function parseComparator(text: string): Interval {
  const m =
    /^(>=|<=|>|<|=|\^|~)?\s*v?(\d+|[xX*])(?:\.(\d+|[xX*]))?(?:\.(\d+|[xX*]))?(?:[-+].*)?$/.exec(
      text,
    );
  if (!m) {
    if (text === '*' || text === '') return ANY;
    throw new Error(`unsupported range ${text}`);
  }
  const op = m[1] ?? '';
  const parts: number[] = [];
  for (const g of [m[2], m[3], m[4]]) {
    if (g === undefined || /^[xX*]$/.test(g)) break;
    parts.push(Number(g));
  }
  if (parts.length === 0) return ANY;
  const floor = pad(parts);
  const next = bump(parts);
  switch (op) {
    case '>=':
      return { ...ANY, lo: floor, loInclusive: true };
    case '>':
      return { ...ANY, lo: next, loInclusive: true };
    case '<':
      return { ...ANY, hi: floor, hiInclusive: false };
    case '<=':
      return { ...ANY, hi: next, hiInclusive: false };
    case '^':
      return { lo: floor, loInclusive: true, hi: caretCeiling(parts), hiInclusive: false };
    case '~':
      return {
        lo: floor,
        loInclusive: true,
        hi: parts.length === 1 ? bump(parts) : ([parts[0]!, parts[1]! + 1, 0] as const),
        hiInclusive: false,
      };
    default:
      return { lo: floor, loInclusive: true, hi: next, hiInclusive: false };
  }
}

function pad(parts: readonly number[]): Version {
  return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0];
}

/** The first version above everything the partial version `parts` names. */
function bump(parts: readonly number[]): Version {
  if (parts.length === 1) return [parts[0]! + 1, 0, 0];
  if (parts.length === 2) return [parts[0]!, parts[1]! + 1, 0];
  return [parts[0]!, parts[1]!, parts[2]! + 1];
}

function caretCeiling(parts: readonly number[]): Version {
  const [major, minor = 0, patch = 0] = parts;
  if (major! > 0) return [major! + 1, 0, 0];
  if (parts.length < 2) return [1, 0, 0];
  if (minor > 0) return [0, minor + 1, 0];
  if (parts.length < 3) return [0, 1, 0];
  return [0, 0, patch + 1];
}

function compareVersions(a: Version, b: Version): number {
  return a[0] - b[0] || a[1] - b[1] || a[2] - b[2];
}

function intersect(a: Interval, b: Interval): Interval {
  const lo = pickLower(a, b);
  const hi = pickUpper(a, b);
  return { lo: lo.v, loInclusive: lo.inclusive, hi: hi.v, hiInclusive: hi.inclusive };
}

function pickLower(a: Interval, b: Interval): { v: Version | null; inclusive: boolean } {
  if (a.lo === null) return { v: b.lo, inclusive: b.loInclusive };
  if (b.lo === null) return { v: a.lo, inclusive: a.loInclusive };
  const c = compareVersions(a.lo, b.lo);
  if (c > 0) return { v: a.lo, inclusive: a.loInclusive };
  if (c < 0) return { v: b.lo, inclusive: b.loInclusive };
  return { v: a.lo, inclusive: a.loInclusive && b.loInclusive };
}

function pickUpper(a: Interval, b: Interval): { v: Version | null; inclusive: boolean } {
  if (a.hi === null) return { v: b.hi, inclusive: b.hiInclusive };
  if (b.hi === null) return { v: a.hi, inclusive: a.hiInclusive };
  const c = compareVersions(a.hi, b.hi);
  if (c < 0) return { v: a.hi, inclusive: a.hiInclusive };
  if (c > 0) return { v: b.hi, inclusive: b.hiInclusive };
  return { v: a.hi, inclusive: a.hiInclusive && b.hiInclusive };
}

function isEmpty(i: Interval): boolean {
  if (i.lo === null || i.hi === null) return false;
  const c = compareVersions(i.lo, i.hi);
  return c > 0 || (c === 0 && !(i.loInclusive && i.hiInclusive));
}

/**
 * Merges alternatives that overlap or touch, so `>=1 <2 || >=2 <4` is the one
 * interval `>=1 <4`. Without it a union that covers a range between its
 * alternatives would look narrower than it is.
 */
function normalize(intervals: readonly Interval[]): Interval[] {
  const sorted = [...intervals].sort(byLowerBound);
  const out: Interval[] = [];
  for (const next of sorted) {
    const last = out[out.length - 1];
    if (last && connects(last, next)) out[out.length - 1] = merge(last, next);
    else out.push(next);
  }
  return out;
}

function byLowerBound(a: Interval, b: Interval): number {
  if (a.lo === null || b.lo === null) return a.lo === b.lo ? 0 : a.lo === null ? -1 : 1;
  const c = compareVersions(a.lo, b.lo);
  if (c !== 0) return c;
  return a.loInclusive === b.loInclusive ? 0 : a.loInclusive ? -1 : 1;
}

/** `a` starts no later than `b`; true when `b` begins inside `a` or exactly where it ends. */
function connects(a: Interval, b: Interval): boolean {
  if (a.hi === null || b.lo === null) return true;
  const c = compareVersions(b.lo, a.hi);
  return c < 0 || (c === 0 && (a.hiInclusive || b.loInclusive));
}

function merge(a: Interval, b: Interval): Interval {
  if (a.hi === null || b.hi === null) return { ...a, hi: null, hiInclusive: true };
  const c = compareVersions(a.hi, b.hi);
  if (c > 0) return a;
  if (c < 0) return { ...a, hi: b.hi, hiInclusive: b.hiInclusive };
  return { ...a, hiInclusive: a.hiInclusive || b.hiInclusive };
}

/** True when every interval of `a` lies inside a single interval of `b`. */
function subset(a: readonly Interval[], b: readonly Interval[]): boolean {
  return a.every((x) => b.some((y) => contains(y, x)));
}

function contains(outer: Interval, inner: Interval): boolean {
  if (outer.lo !== null) {
    if (inner.lo === null) return false;
    const c = compareVersions(inner.lo, outer.lo);
    if (c < 0 || (c === 0 && inner.loInclusive && !outer.loInclusive)) return false;
  }
  if (outer.hi !== null) {
    if (inner.hi === null) return false;
    const c = compareVersions(inner.hi, outer.hi);
    if (c > 0 || (c === 0 && inner.hiInclusive && !outer.hiInclusive)) return false;
  }
  return true;
}
