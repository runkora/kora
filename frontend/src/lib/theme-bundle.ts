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

/** The one method of a file handle the bounded read needs. */
export interface ReadableHandle {
  read(
    buffer: Buffer,
    offset: number,
    length: number,
    position: number,
  ): Promise<{ bytesRead: number }>;
}

/**
 * Reads at most `maxBytes + 1` bytes from the start of a file, so an oversize
 * file is seen without being loaded. A read may return fewer bytes than asked
 * for, so it loops to end of file or the bound.
 */
export async function readUpTo(handle: ReadableHandle, maxBytes: number): Promise<Uint8Array> {
  const buffer = Buffer.alloc(maxBytes + 1);
  let total = 0;
  while (total < buffer.length) {
    const { bytesRead } = await handle.read(buffer, total, buffer.length - total, total);
    if (bytesRead === 0) break;
    total += bytesRead;
  }
  return new Uint8Array(buffer.subarray(0, total));
}

async function readBounded(file: string, maxBytes: number): Promise<Uint8Array> {
  const handle = await open(file, 'r');
  try {
    return await readUpTo(handle, maxBytes);
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
