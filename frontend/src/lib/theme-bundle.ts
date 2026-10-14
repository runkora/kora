/**
 * The bounded reads of a theme bundle, shared by the server module and the init
 * container's `scripts/validate-theme.ts` so both hold a bundle to the same
 * limits. Node built-ins only: the init container bundles this file without
 * Next or `server-only`.
 */
import { open, realpath } from 'node:fs/promises';
import path from 'node:path';

/** The largest `theme.yaml` either reader parses. A theme is a few dozen lines. */
export const MAX_THEME_YAML_BYTES = 64 * 1024;

/** Thrown when `theme.yaml` is larger than {@link MAX_THEME_YAML_BYTES}. */
export class ThemeYamlTooLargeError extends Error {
  constructor() {
    super(`is larger than the ${MAX_THEME_YAML_BYTES} byte limit for a theme document`);
    this.name = 'ThemeYamlTooLargeError';
  }
}

/** ENOENT, which callers read as "no such file". */
export function isMissing(err: unknown): boolean {
  return typeof err === 'object' && err !== null && (err as { code?: string }).code === 'ENOENT';
}

// What reading a bundle path can throw when the path is not a readable file in
// the bundle: absent, a directory, through a file, a symlink loop or a NUL.
const UNREADABLE_CODES = new Set([
  'ENOENT',
  'ENOTDIR',
  'EISDIR',
  'ELOOP',
  'ENAMETOOLONG',
  'ERR_INVALID_ARG_VALUE',
]);

function isUnreadable(err: unknown): boolean {
  const code = typeof err === 'object' && err !== null ? (err as { code?: string }).code : '';
  return code !== undefined && UNREADABLE_CODES.has(code);
}

/** Reads at most `maxBytes + 1` bytes of a file, so an oversize file is seen without being loaded. */
async function readBounded(file: string, maxBytes: number): Promise<Uint8Array> {
  const handle = await open(file, 'r');
  try {
    const buffer = Buffer.alloc(maxBytes + 1);
    const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
    return new Uint8Array(buffer.subarray(0, bytesRead));
  } finally {
    await handle.close();
  }
}

/**
 * Reads a theme document as UTF-8, refusing one larger than
 * {@link MAX_THEME_YAML_BYTES} before it is parsed. Throws the underlying error
 * when the file is absent.
 */
export async function readThemeDocument(file: string): Promise<string> {
  const bytes = await readBounded(file, MAX_THEME_YAML_BYTES);
  if (bytes.length > MAX_THEME_YAML_BYTES) throw new ThemeYamlTooLargeError();
  return new TextDecoder('utf-8').decode(bytes);
}

/**
 * Reads one asset by bundle-relative path; null when the bundle has no such
 * readable file. The path is resolved through symlinks, and a file whose real
 * location is outside the real bundle root is treated as absent, so a link in
 * the bundle cannot lead the reader out of it. The validator has already
 * refused paths that leave the bundle; this is the belt to that brace.
 */
export async function readBundleAsset(
  dir: string,
  assetPath: string,
  maxBytes: number,
): Promise<Uint8Array | null> {
  try {
    const root = await realpath(dir);
    const real = await realpath(path.resolve(dir, 'assets', assetPath));
    if (!real.startsWith(root + path.sep)) return null;
    return await readBounded(real, maxBytes);
  } catch (err) {
    if (isUnreadable(err)) return null;
    throw err;
  }
}

/**
 * The identity values a theme still takes from kora while overriding others: a
 * mix of two brands. Empty when the theme sets none (it keeps kora's identity on
 * purpose) or all of them. Reads the validator's `mixedIdentity` when the report
 * carries it and otherwise derives it from `fallThrough`: the identity values
 * are `productName` plus every mode of every image slot.
 */
export function mixedIdentity(
  report: { readonly fallThrough: readonly string[]; readonly mixedIdentity?: readonly string[] },
  manifest: {
    readonly slots: readonly { readonly kind: string; readonly modes: readonly string[] }[];
  },
): readonly string[] {
  if (report.mixedIdentity !== undefined) return report.mixedIdentity;
  const total = manifest.slots.reduce((n, s) => (s.kind === 'image' ? n + s.modes.length : n), 1);
  const missing = report.fallThrough.length;
  return missing > 0 && missing < total ? report.fallThrough : [];
}
