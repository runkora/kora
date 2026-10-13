/**
 * The theme validator and loader: one function that takes the document text,
 * the manifest, the instance's contract version and an asset reader, and returns
 * either the validated theme or every fault it found. Pure: it never touches
 * the filesystem or the network, so the same code runs in the init container,
 * the frontend's server module and a browser preview. See the white-labelling
 * theme ADR, Decisions 1 and 3.
 */
import { parse as parseYaml } from 'yaml';

import type { Mode } from '../tokens/surface';
import { contentFaults, extensionOf } from './assets';
import {
  FORBIDDEN_IN_VALUES,
  contractMajor,
  isBundlePath,
  isContractVersion,
  matchesFormat,
  pattern,
} from './formats';
import { themeHash } from './hash';
import { familyTokenForSlot, type ThemeManifest, type ThemeManifestSlot } from './manifest';
import type {
  IgnoredEntry,
  Theme,
  ThemeError,
  ThemeReport,
  ThemeSlot,
  ThemeTokenValues,
  ValidationResult,
} from './types';

/** Reads an asset by bundle-relative path; null when the bundle has no such file. */
export type AssetReader = (path: string) => Promise<Uint8Array | null>;

export interface ValidateOptions {
  readonly manifest: ThemeManifest;
  /** The instance's contract version, `MAJOR.MINOR`; the document's major must equal its major. */
  readonly contractVersion: string;
  readonly readAsset: AssetReader;
}

const TOP_LEVEL_KEYS = ['contractVersion', 'productName', 'tokens', 'slots'] as const;

export async function validateTheme(
  text: string,
  options: ValidateOptions,
): Promise<ValidationResult> {
  const { manifest, readAsset } = options;
  const errors: ThemeError[] = [];
  const ignored: IgnoredEntry[] = [];
  const emptyReport = (): ThemeReport => ({
    ignored,
    fallThrough: fallThrough(manifest, undefined, {}),
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

  const contractVersion = checkContractVersion(
    parsed.contractVersion,
    options.contractVersion,
    errors,
  );
  const productName = checkProductName(parsed.productName, manifest, errors);
  const tokens = checkTokens(parsed.tokens, manifest, errors, ignored);
  const slots = checkSlots(parsed.slots, manifest, errors, ignored);

  for (const slotName of Object.keys(slots)) {
    const token = familyTokenForSlot(manifest, slotName);
    if (token && token in tokens) {
      errors.push({
        path: `tokens.${token}`,
        message: `set by both the token and the ${slotName} slot; a font slot assigns the family itself`,
      });
    }
  }

  const assets = await readAssets(slots, manifest, readAsset, errors);
  const report: ThemeReport = { ignored, fallThrough: fallThrough(manifest, productName, slots) };

  if (errors.length > 0 || contractVersion === null) {
    return { ok: false, errors, report };
  }

  const theme: Theme = {
    contractVersion,
    ...(productName === undefined ? {} : { productName }),
    tokens,
    slots,
    assets,
    hash: await themeHash(text, assets),
  };
  return { ok: true, theme, report };
}

// ─── Fields ──────────────────────────────────────────────────────────────────

function checkContractVersion(
  value: unknown,
  running: string,
  errors: ThemeError[],
): string | null {
  if (value === undefined || value === null) {
    errors.push({
      path: 'contractVersion',
      message: 'required: the contract version the theme was written against, such as "1.0"',
    });
    return null;
  }
  // YAML reads an unquoted 1.0 as the number 1 and 1.10 as 1.1, so a number
  // can never be trusted to be the version its author wrote.
  if (typeof value !== 'string' || !isContractVersion(value)) {
    errors.push({
      path: 'contractVersion',
      message:
        typeof value === 'number'
          ? `must be a quoted string such as "1.0": YAML reads an unquoted ${value} as a number`
          : 'must be MAJOR.MINOR such as "1.0"',
    });
    return null;
  }
  const major = contractMajor(value);
  const runningMajor = contractMajor(running);
  if (major !== runningMajor) {
    errors.push({
      path: 'contractVersion',
      message: `contract major ${major} does not match the running contract ${running}; this instance accepts contract major ${runningMajor} only`,
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
  if (name.length < minLength || name.length > maxLength) {
    errors.push({
      path: 'productName',
      message: `must be ${minLength} to ${maxLength} characters after trimming; got ${name.length}`,
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
  if (value === undefined) return out;
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
  return raw;
}

function checkSlots(
  value: unknown,
  manifest: ThemeManifest,
  errors: ThemeError[],
  ignored: IgnoredEntry[],
): Record<string, ThemeSlot> {
  const out: Record<string, ThemeSlot> = {};
  if (value === undefined) return out;
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
    if (slot.modes.length === 0) {
      const checked = checkPath(entry, path, errors);
      if (checked !== undefined) out[name] = { kind: 'font', path: checked };
      continue;
    }
    if (!isPlainObject(entry)) {
      errors.push({ path, message: `must be a map of mode to path (${slot.modes.join(', ')})` });
      continue;
    }
    const paths: Partial<Record<Mode, string>> = {};
    for (const [mode, raw] of Object.entries(entry)) {
      const modePath = `${path}.${mode}`;
      if (!(slot.modes as readonly string[]).includes(mode)) {
        errors.push({
          path: modePath,
          message: `unknown mode; the slot takes ${slot.modes.join(' and ')}`,
        });
        continue;
      }
      const checked = checkPath(raw, modePath, errors);
      if (checked !== undefined) paths[mode as Mode] = checked;
    }
    if (Object.keys(paths).length > 0) out[name] = { kind: 'image', paths };
  }
  return out;
}

function checkPath(raw: unknown, path: string, errors: ThemeError[]): string | undefined {
  if (raw === null) {
    errors.push({ path, message: 'cannot be set to nothing; omit it to keep the default' });
    return undefined;
  }
  if (typeof raw !== 'string') {
    errors.push({ path, message: 'must be a path relative to the bundle root' });
    return undefined;
  }
  if (!isBundlePath(raw)) {
    errors.push({
      path,
      message: `"${raw}" is not a bundle-relative path: no leading slash, no "..", no backslash`,
    });
    return undefined;
  }
  return raw;
}

// ─── Assets ──────────────────────────────────────────────────────────────────

async function readAssets(
  slots: Readonly<Record<string, ThemeSlot>>,
  manifest: ThemeManifest,
  readAsset: AssetReader,
  errors: ThemeError[],
): Promise<Map<string, Uint8Array>> {
  const assets = new Map<string, Uint8Array>();
  const bySlot = new Map(manifest.slots.map((s) => [s.name, s]));
  const references: { path: string; docPath: string; slot: ThemeManifestSlot }[] = [];
  for (const [name, slot] of Object.entries(slots)) {
    const declared = bySlot.get(name)!;
    if (slot.kind === 'font') {
      references.push({ path: slot.path, docPath: `slots.${name}`, slot: declared });
    } else {
      for (const [mode, path] of Object.entries(slot.paths)) {
        references.push({ path, docPath: `slots.${name}.${mode}`, slot: declared });
      }
    }
  }

  const bytesByPath = new Map<string, Uint8Array | null>();
  for (const { path } of references) {
    if (!bytesByPath.has(path)) bytesByPath.set(path, await readAsset(path));
  }

  for (const { path, docPath, slot } of references) {
    const extension = extensionOf(path);
    if (extension === null || !slot.formats.includes(extension)) {
      errors.push({
        path: docPath,
        message: `"${path}" must have one of the extensions ${slot.formats.join(', ')}`,
      });
      continue;
    }
    const bytes = bytesByPath.get(path) ?? null;
    if (bytes === null) {
      errors.push({ path: docPath, message: `"${path}" is not in the bundle` });
      continue;
    }
    if (bytes.length > slot.maxBytes) {
      errors.push({
        path: docPath,
        message: `"${path}" is ${bytes.length} bytes; the ${slot.name} slot allows at most ${slot.maxBytes}`,
      });
      continue;
    }
    const faults = contentFaults(extension, bytes);
    if (faults.length > 0) {
      errors.push({ path: docPath, message: `"${path}" ${faults.join('; ')}` });
      continue;
    }
    assets.set(path, bytes);
  }
  return assets;
}

// ─── Report ──────────────────────────────────────────────────────────────────

/**
 * Identity values the theme leaves to the kora default: the product name and
 * every mode of every image slot. The PRD's identity goal is met only when
 * this list is empty.
 */
function fallThrough(
  manifest: ThemeManifest,
  productName: string | undefined,
  slots: Readonly<Record<string, ThemeSlot>>,
): string[] {
  const out: string[] = [];
  if (productName === undefined) out.push('productName');
  for (const slot of manifest.slots) {
    if (slot.kind !== 'image') continue;
    const set = slots[slot.name];
    for (const mode of slot.modes) {
      if (!(set && set.kind === 'image' && set.paths[mode] !== undefined)) {
        out.push(`${slot.name}.${mode}`);
      }
    }
  }
  return out;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
