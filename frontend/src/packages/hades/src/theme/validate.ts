/** Validates a theme document and returns the theme or every fault found. Pure: it never fetches a URL. */
import { parse as parseYaml } from 'yaml';

import type { Mode } from '../tokens/surface';
import { contrastRatio } from './contrast';
import {
  FORBIDDEN_IN_VALUES,
  fontFamilyFault,
  hasAssetExtension,
  isAssetUrl,
  isKoraVersion,
  koraMajor,
  matchesFormat,
  pattern,
} from './formats';
import { themeHash } from './hash';
import { familyTokenForSlot, type ThemeManifest, type ThemeManifestSlot } from './manifest';
import type {
  ContrastWarning,
  IgnoredEntry,
  Theme,
  ThemeError,
  ThemeReport,
  ThemeSlot,
  ThemeTokenValues,
  ValidationResult,
} from './types';

/** Raw token to value, per mode: the value the app ships for every settable token, from `defaults.json`. */
export type TokenDefaults = Readonly<Partial<Record<Mode, Readonly<Record<string, string>>>>>;

export interface ValidateOptions {
  readonly manifest: ThemeManifest;
  /** Raw defaults per mode. Without them a pair is checked only when the theme sets both sides. */
  readonly defaults?: TokenDefaults;
  /** The instance's kora version without the `v`. Null for a development build, which skips the major check. */
  readonly koraVersion: string | null;
}

const TOP_LEVEL_KEYS = ['koraVersion', 'productName', 'tokens', 'slots'] as const;

export async function validateTheme(
  text: string,
  options: ValidateOptions,
): Promise<ValidationResult> {
  const { manifest } = options;
  const errors: ThemeError[] = [];
  const ignored: IgnoredEntry[] = [];
  const versionCheckSkipped = options.koraVersion === null;
  const emptyReport = (): ThemeReport => ({
    ignored,
    versionCheckSkipped,
    ...identityReport(manifest, undefined, {}),
    contrastWarnings: [],
  });

  let parsed: unknown;
  try {
    parsed = parseYaml(text);
  } catch (err) {
    errors.push({ path: '', message: `not valid YAML: ${(err as Error).message}` });
    return { ok: false, errors, report: emptyReport() };
  }
  if (!isPlainObject(parsed)) {
    errors.push({ path: '', message: 'the document must be a map of top-level keys' });
    return { ok: false, errors, report: emptyReport() };
  }

  for (const key of Object.keys(parsed)) {
    if (!(TOP_LEVEL_KEYS as readonly string[]).includes(key)) {
      errors.push({ path: key, message: 'unknown top-level key' });
    }
  }

  const koraVersion = checkKoraVersion(parsed.koraVersion, options.koraVersion, errors);
  const productName = checkProductName(parsed.productName, manifest, errors);
  const tokens = checkTokens(parsed.tokens, manifest, errors, ignored);
  const slots = checkSlots(parsed.slots, manifest, errors, ignored);
  const contrastWarnings = checkContrast(tokens, manifest, options.defaults, errors);

  for (const slotName of Object.keys(slots)) {
    const token = familyTokenForSlot(manifest, slotName);
    if (token && token in tokens) {
      errors.push({
        path: `tokens.${token}`,
        message: `set by both the token and the ${slotName} slot; a font slot assigns the family itself`,
      });
    }
  }

  const report: ThemeReport = {
    ignored,
    versionCheckSkipped,
    ...identityReport(manifest, productName, slots),
    contrastWarnings,
  };

  if (errors.length > 0 || koraVersion === null) {
    return { ok: false, errors, report };
  }

  const theme: Theme = {
    koraVersion,
    ...(productName === undefined ? {} : { productName }),
    tokens,
    slots,
    hash: await themeHash(text),
  };
  return { ok: true, theme, report };
}

// ─── Fields ──────────────────────────────────────────────────────────────────

function checkKoraVersion(
  value: unknown,
  running: string | null,
  errors: ThemeError[],
): string | null {
  if (value === undefined || value === null) {
    errors.push({
      path: 'koraVersion',
      message: 'required: the kora release the theme was written against, such as "1.4.0"',
    });
    return null;
  }
  // YAML reads an unquoted 1.10 as the number 1.1, so a number is never accepted.
  if (typeof value !== 'string' || !isKoraVersion(value)) {
    errors.push({
      path: 'koraVersion',
      message:
        typeof value === 'number'
          ? `must be a quoted string such as "1.4.0": YAML reads an unquoted ${value} as a number`
          : 'must be MAJOR.MINOR.PATCH such as "1.4.0"',
    });
    return null;
  }
  // A development build has no major to compare with.
  if (running === null) return value;
  const runningMajor = koraMajor(running);
  if (runningMajor === null) {
    errors.push({
      path: '',
      message: `this instance cannot tell which kora release it is ("${running}"), so it cannot compare the theme's koraVersion; run a released image, or leave the version unset in a development build`,
    });
    return null;
  }
  const major = koraMajor(value)!;
  if (major !== runningMajor) {
    errors.push({
      path: 'koraVersion',
      message: `the theme was written against kora ${value} (major ${major}), but this instance is kora ${running}, which accepts major ${runningMajor} only; rewrite the theme against a ${runningMajor}.x release`,
    });
    return null;
  }
  return value;
}

function checkProductName(
  value: unknown,
  manifest: ThemeManifest,
  errors: ThemeError[],
): string | undefined {
  if (value === undefined) return undefined;
  if (value === null) {
    errors.push({
      path: 'productName',
      message: 'cannot be set to nothing; omit it to keep the default',
    });
    return undefined;
  }
  if (typeof value !== 'string') {
    errors.push({ path: 'productName', message: 'must be a string' });
    return undefined;
  }
  const { minLength, maxLength, pattern: source } = manifest.productName;
  const name = value.trim();
  const length = [...name].length;
  if (length < minLength || length > maxLength) {
    errors.push({
      path: 'productName',
      message: `must be ${minLength} to ${maxLength} characters after trimming; got ${length}`,
    });
    return undefined;
  }
  if (!pattern(source).test(name)) {
    errors.push({
      path: 'productName',
      message: "may contain only letters, digits, spaces and the punctuation . - & '",
    });
    return undefined;
  }
  return name;
}

function checkTokens(
  value: unknown,
  manifest: ThemeManifest,
  errors: ThemeError[],
  ignored: IgnoredEntry[],
): Record<string, ThemeTokenValues> {
  const out: Record<string, ThemeTokenValues> = {};
  if (value === undefined || value === null) return out;
  if (!isPlainObject(value)) {
    errors.push({ path: 'tokens', message: 'must be a map keyed by token name' });
    return out;
  }
  const known = new Map(manifest.tokens.map((t) => [t.name, t]));
  for (const [name, entry] of Object.entries(value)) {
    const token = known.get(name);
    if (!token) {
      ignored.push({ kind: 'token', name });
      continue;
    }
    const path = `tokens.${name}`;
    if (!isPlainObject(entry)) {
      errors.push({ path, message: `must be a map of mode to value (${token.modes.join(', ')})` });
      continue;
    }
    const values: Partial<Record<Mode, string>> = {};
    for (const [mode, raw] of Object.entries(entry)) {
      const modePath = `${path}.${mode}`;
      if (!(token.modes as readonly string[]).includes(mode)) {
        errors.push({
          path: modePath,
          message: `unknown mode; the token takes ${token.modes.join(' and ')}`,
        });
        continue;
      }
      const checked = checkTokenValue(raw, token.format, modePath, errors);
      if (checked !== undefined) values[mode as Mode] = checked;
    }
    if (Object.keys(values).length > 0) out[name] = values;
  }
  return out;
}

function checkTokenValue(
  raw: unknown,
  format: ThemeManifest['tokens'][number]['format'],
  path: string,
  errors: ThemeError[],
): string | undefined {
  if (raw === null) {
    errors.push({ path, message: 'cannot be set to nothing; omit the mode to keep the default' });
    return undefined;
  }
  if (typeof raw !== 'string') {
    errors.push({ path, message: `must be a string in the ${format} format` });
    return undefined;
  }
  if (FORBIDDEN_IN_VALUES.test(raw)) {
    errors.push({ path, message: 'contains a character or function no token value may carry' });
    return undefined;
  }
  if (!matchesFormat(format, raw)) {
    errors.push({ path, message: `"${raw}" is not a valid ${format} value` });
    return undefined;
  }
  const fault = format === 'font-family-name' ? fontFamilyFault(raw) : null;
  if (fault) {
    errors.push({ path, message: `"${raw}" ${fault}` });
    return undefined;
  }
  return raw;
}

function checkSlots(
  value: unknown,
  manifest: ThemeManifest,
  errors: ThemeError[],
  ignored: IgnoredEntry[],
): Record<string, ThemeSlot> {
  const out: Record<string, ThemeSlot> = {};
  if (value === undefined || value === null) return out;
  if (!isPlainObject(value)) {
    errors.push({ path: 'slots', message: 'must be a map keyed by slot name' });
    return out;
  }
  const known = new Map(manifest.slots.map((s) => [s.name, s]));
  for (const [name, entry] of Object.entries(value)) {
    const slot = known.get(name);
    if (!slot) {
      ignored.push({ kind: 'slot', name });
      continue;
    }
    const path = `slots.${name}`;
    if (slot.kind === 'font') {
      const checked = checkUrl(entry, path, slot, errors);
      if (checked !== undefined) out[name] = { kind: 'font', url: checked };
      continue;
    }
    if (!isPlainObject(entry)) {
      errors.push({ path, message: `must be a map of mode to URL (${slot.modes.join(', ')})` });
      continue;
    }
    const urls: Partial<Record<Mode, string>> = {};
    for (const [mode, raw] of Object.entries(entry)) {
      const modePath = `${path}.${mode}`;
      if (!(slot.modes as readonly string[]).includes(mode)) {
        errors.push({
          path: modePath,
          message: `unknown mode; the slot takes ${slot.modes.join(' and ')}`,
        });
        continue;
      }
      const checked = checkUrl(raw, modePath, slot, errors);
      if (checked !== undefined) urls[mode as Mode] = checked;
    }
    if (Object.keys(urls).length > 0) out[name] = { kind: 'image', urls };
  }
  return out;
}

function checkUrl(
  raw: unknown,
  path: string,
  slot: ThemeManifestSlot,
  errors: ThemeError[],
): string | undefined {
  if (raw === null) {
    errors.push({ path, message: 'cannot be set to nothing; omit it to keep the default' });
    return undefined;
  }
  if (typeof raw !== 'string') {
    errors.push({ path, message: 'must be an https:// URL' });
    return undefined;
  }
  if (!isAssetUrl(raw)) {
    errors.push({
      path,
      message: `"${raw}" is not an https:// URL of at most 2048 characters with a plain host, no credentials, no fragment and no quotes, parentheses or whitespace`,
    });
    return undefined;
  }
  if (!hasAssetExtension(raw, slot.formats)) {
    errors.push({
      path,
      message: `"${raw}" must end in one of the extensions ${slot.formats.join(', ')}, before any query`,
    });
    return undefined;
  }
  return raw;
}

// ─── Contrast ────────────────────────────────────────────────────────────────

// Checks each contrast pair per mode. A failed `error` pair goes to `errors`, a `warn` pair is returned.
// A pair is skipped when the theme sets neither side (the app's own colours are not its concern),
// a side has no hex or oklch value, or `defaults` is missing and only one side is set.
function checkContrast(
  tokens: Readonly<Record<string, ThemeTokenValues>>,
  manifest: ThemeManifest,
  defaults: TokenDefaults | undefined,
  errors: ThemeError[],
): ContrastWarning[] {
  const warnings: ContrastWarning[] = [];
  const rawOf = (name: string): string | null => {
    const entry = manifest.mapping?.[name];
    return entry && !entry.derived && entry.tokens.length === 1 ? entry.tokens[0]! : null;
  };
  for (const pair of manifest.contrast ?? []) {
    const text = rawOf(pair.text);
    if (text === null) continue;
    for (const mode of manifest.modes) {
      const textSet = tokens[text]?.[mode];
      const textValue = textSet ?? defaults?.[mode]?.[text];
      if (textValue === undefined) continue;
      for (const backdrop of pair.on) {
        const on = rawOf(backdrop);
        if (on === null) continue;
        const onSet = tokens[on]?.[mode];
        const onValue = onSet ?? defaults?.[mode]?.[on];
        if (onValue === undefined) continue;
        if (textSet === undefined && onSet === undefined) continue;
        if (defaults === undefined && (textSet === undefined || onSet === undefined)) continue;
        const ratio = contrastRatio(textValue, onValue);
        if (ratio === null || ratio >= pair.min) continue;
        // Floored, not rounded: 4.496 must not print as "4.50:1, below the 4.5:1 minimum".
        const fault = {
          path: textSet !== undefined ? `tokens.${text}.${mode}` : `tokens.${on}.${mode}`,
          message:
            `${pair.text} on ${backdrop} in ${mode} has contrast ${(Math.floor(ratio * 100) / 100).toFixed(2)}:1, ` +
            `below the ${pair.min}:1 minimum (contrast pair ${pair.id}; ` +
            `${text} ${textValue} on ${on} ${onValue})`,
        };
        (pair.severity === 'error' ? errors : warnings).push(fault);
      }
    }
  }
  return warnings;
}

// ─── Report ──────────────────────────────────────────────────────────────────

/**
 * Lists the identity values left to the kora default: the product name and every mode of every image slot.
 * `mixedIdentity` is that list when some are set and some are not, which would show two brands.
 */
function identityReport(
  manifest: ThemeManifest,
  productName: string | undefined,
  slots: Readonly<Record<string, ThemeSlot>>,
): Pick<ThemeReport, 'fallThrough' | 'mixedIdentity'> {
  const fallThrough: string[] = [];
  let total = 1;
  if (productName === undefined) fallThrough.push('productName');
  for (const slot of manifest.slots) {
    if (slot.kind !== 'image') continue;
    const set = slots[slot.name];
    for (const mode of slot.modes) {
      total++;
      if (!(set && set.kind === 'image' && set.urls[mode] !== undefined)) {
        fallThrough.push(`${slot.name}.${mode}`);
      }
    }
  }
  const mixed = fallThrough.length > 0 && fallThrough.length < total;
  return { fallThrough, mixedIdentity: mixed ? fallThrough : [] };
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
