/**
 * The value syntaxes a theme may supply, as regex sources.
 * The validator and `scripts/lib/schema.ts` both use them, so the schema cannot drift.
 */
import type { TokenFormat } from '../tokens/surface';

const NUMBER = '(?:\\d+(?:\\.\\d*)?|\\.\\d+)';
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

/** Anchored regex source per token format, so a value matches in full or is rejected. */
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
function familyEntries(value: string): string[] {
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
 * An `https://` URL of at most 2048 characters: plain host, no credentials or fragment, nothing CSS or HTML must escape.
 * `hasAssetExtension` checks the extension, because it depends on the slot.
 */
export const ASSET_URL_PATTERN =
  '^(?=.{1,2048}$)https://[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::\\d{1,5})?(?:/[^\\s?#\\\\"\'<>()\\u0000-\\u001f]*)?(?:\\?[^\\s#\\\\"\'<>()\\u0000-\\u001f]*)?$';

/** Characters no token value may carry; checked first so the error names the injection, not the syntax. */
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

/** The major of a kora version; accepts a leading `v` and a pre-release suffix, as a release tag has. */
export function koraMajor(value: string): number | null {
  const m = /^v?(\d+)\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.exec(value);
  return m ? Number(m[1]) : null;
}

export function isAssetUrl(value: string): boolean {
  return pattern(ASSET_URL_PATTERN).test(value);
}

/** A pattern matching a URL whose path ends in one of the extensions, in any case; a query never counts. */
export function assetExtensionPattern(formats: readonly string[]): string {
  const folded = formats.map((f) =>
    [...f].map((c) => (/[a-z]/i.test(c) ? `[${c.toLowerCase()}${c.toUpperCase()}]` : c)).join(''),
  );
  return `^https://[^/?]+(?:/[^/?]*)*/[^/?]+\\.(?:${folded.join('|')})(?:\\?.*)?$`;
}

export function hasAssetExtension(url: string, formats: readonly string[]): boolean {
  return pattern(assetExtensionPattern(formats)).test(url);
}
