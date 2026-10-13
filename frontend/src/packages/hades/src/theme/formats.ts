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
  'font-family-name': `^[\\p{L}\\p{N} \\-_'",.]{1,64}$`,
};

/** The contract version: `MAJOR.MINOR`, no patch, no leading zeros. */
export const CONTRACT_VERSION_PATTERN = '^(?:0|[1-9]\\d*)\\.(?:0|[1-9]\\d*)$';

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

export function isContractVersion(value: string): boolean {
  return pattern(CONTRACT_VERSION_PATTERN).test(value);
}

export function contractMajor(value: string): number | null {
  const m = /^(\d+)\./.exec(value);
  return m ? Number(m[1]) : null;
}

export function isBundlePath(value: string): boolean {
  return pattern(BUNDLE_PATH_PATTERN).test(value);
}
