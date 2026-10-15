/**
 * The value syntaxes a theme may supply, as regular-expression sources. Each
 * is the one definition of its format: the validator compiles it with the `u`
 * flag, and `scripts/lib/schema.ts` writes the same source into
 * `theme.schema.json`, whose patterns are ECMA-262 with Unicode semantics. A
 * rule therefore never exists twice. See the white-labelling theme ADR,
 * Decision 1.
 */
import type { TokenFormat } from '../tokens/surface';

const NUMBER = '(?:\\d+\\.?\\d*|\\.\\d+)';
const SIGNED = `[+-]?${NUMBER}`;

/** `#rgb`, `#rgba`, `#rrggbb` or `#rrggbbaa`. */
const HEX = '#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})';

/** One OKLCH channel: a number, a percentage or `none`. */
const CHANNEL = `(?:none|${SIGNED}%?)`;

/** `oklch(L C H)` or `oklch(L C H / A)`. */
const OKLCH = `oklch\\(\\s*${CHANNEL}\\s+${CHANNEL}\\s+${CHANNEL}\\s*(?:/\\s*${CHANNEL}\\s*)?\\)`;

/** `rgb(...)` or `rgba(...)` with numbers, percentages, commas, slashes and spaces inside. */
const RGB = 'rgba?\\([\\d.%\\s,/]+\\)';

const SHADOW_COLOR = `(?:${HEX}|${OKLCH}|${RGB})`;

/** A shadow offset, blur or spread: `0` or a signed number in px, rem or em. */
const SHADOW_LENGTH = `(?:0|${SIGNED}(?:px|rem|em))`;
const SHADOW_LENGTHS = `${SHADOW_LENGTH}(?:\\s+${SHADOW_LENGTH}){1,3}`;

/** One shadow: optional `inset`, two to four lengths, an optional colour before or after them. */
const SHADOW = `(?:inset\\s+)?(?:${SHADOW_COLOR}\\s+${SHADOW_LENGTHS}|${SHADOW_LENGTHS}(?:\\s+${SHADOW_COLOR})?)`;

/**
 * Regular-expression source per token format. Anchored, so a value is either
 * entirely one of the accepted syntaxes or malformed; nothing a browser would
 * interpret as a second declaration, a block or a function call can pass.
 */
export const FORMAT_PATTERNS: Readonly<Record<TokenFormat, string>> = {
  'hex-or-oklch': `^(?:${HEX}|${OKLCH})$`,
  'css-shadow-list': `^${SHADOW}(?:\\s*,\\s*${SHADOW}){0,3}$`,
  'css-length': `^(?:0|${NUMBER}(?:px|rem|em|%))$`,
  'font-family-name': `^[\\p{L}\\p{N}\\p{M} \\-_,.]{1,64}$`,
};

/** Generic family keywords: valid unquoted in a `font-family` list, and a name when quoted. */
const GENERIC_FAMILIES: ReadonlySet<string> = new Set([
  'serif',
  'sans-serif',
  'monospace',
  'cursive',
  'fantasy',
  'system-ui',
  'ui-serif',
  'ui-sans-serif',
  'ui-monospace',
  'ui-rounded',
  'math',
  'emoji',
  'fangsong',
]);

/** CSS-wide keywords (and `default`), which are not valid family names. */
const RESERVED_FAMILIES: ReadonlySet<string> = new Set([
  'inherit',
  'initial',
  'unset',
  'revert',
  'revert-layer',
  'default',
]);

/** The trimmed entries of a comma-separated family list; empty entries are kept so callers can reject them. */
export function familyEntries(value: string): string[] {
  return value.split(',').map((entry) => entry.trim());
}

/** Why a `font-family-name` value cannot be emitted as a valid `font-family` list, or null. */
export function fontFamilyFault(value: string): string | null {
  for (const entry of familyEntries(value)) {
    if (entry === '') return 'has an empty entry; separate families with single commas';
    if (RESERVED_FAMILIES.has(entry.toLowerCase())) {
      return `"${entry}" is a CSS keyword, not a font family name`;
    }
  }
  return null;
}

/** A validated family list as a `font-family` value: names quoted, generic keywords left bare. */
export function fontFamilyList(value: string, quote: (name: string) => string): string {
  return familyEntries(value)
    .map((entry) =>
      GENERIC_FAMILIES.has(entry.toLowerCase()) ? entry.toLowerCase() : `"${quote(entry)}"`,
    )
    .join(', ');
}

/** A kora release a theme was written against: `MAJOR.MINOR.PATCH`, no leading zeros, no pre-release suffix. */
export const KORA_VERSION_PATTERN = '^(?:0|[1-9]\\d*)\\.(?:0|[1-9]\\d*)\\.(?:0|[1-9]\\d*)$';

/**
 * A bundle-relative path: `/`-separated segments, none empty, none `.` or
 * `..`, no backslash, no leading slash.
 */
export const BUNDLE_PATH_PATTERN = '^(?:(?!\\.\\.?/)[^/\\\\]+/)*(?!\\.\\.?$)[^/\\\\]+$';

/**
 * Characters no token value may carry whatever its format, checked before the
 * format so the message names the injection attempt rather than the syntax.
 */
export const FORBIDDEN_IN_VALUES = /[;{}\\<]|url\(|var\(/;

const compiled = new Map<string, RegExp>();

/** Compiles a pattern source with the `u` flag, once. */
export function pattern(source: string): RegExp {
  let re = compiled.get(source);
  if (!re) {
    re = new RegExp(source, 'u');
    compiled.set(source, re);
  }
  return re;
}

export function matchesFormat(format: TokenFormat, value: string): boolean {
  return pattern(FORMAT_PATTERNS[format]).test(value);
}

export function isKoraVersion(value: string): boolean {
  return pattern(KORA_VERSION_PATTERN).test(value);
}

/**
 * The major of a kora version. Tolerates a leading `v` and a pre-release
 * suffix, because the instance's own version is its release tag: `0.8.0-rc.2`
 * is major 0.
 */
export function koraMajor(value: string): number | null {
  const m = /^v?(\d+)\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.exec(value);
  return m ? Number(m[1]) : null;
}

export function isBundlePath(value: string): boolean {
  return pattern(BUNDLE_PATH_PATTERN).test(value);
}
