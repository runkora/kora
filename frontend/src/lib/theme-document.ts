/**
 * The bounded read of a theme document, shared by the server module and the
 * init container's `scripts/validate-theme.ts` so both hold a document to the
 * same limit. Node built-ins only: the init container bundles this file
 * without Next or `server-only`.
 */
import { open } from 'node:fs/promises';

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

/**
 * Reads a theme document as UTF-8, refusing one larger than
 * {@link MAX_THEME_YAML_BYTES} before it is parsed. Throws the underlying error
 * when the file is absent.
 */
export async function readThemeDocument(file: string): Promise<string> {
  const handle = await open(file, 'r');
  try {
    const bytes = await readUpTo(handle, MAX_THEME_YAML_BYTES);
    if (bytes.length > MAX_THEME_YAML_BYTES) throw new ThemeYamlTooLargeError();
    return new TextDecoder('utf-8').decode(bytes);
  } finally {
    await handle.close();
  }
}
