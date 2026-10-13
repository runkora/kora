/**
 * The theme hash: SHA-256 over the document text and every referenced asset,
 * length-prefixed and in a fixed order, so the same bundle always hashes the
 * same and any byte change in the document or an asset changes it. Decision 4
 * of the white-labelling theme ADR puts it in the asset route's URL.
 */

const encoder = new TextEncoder();

export async function themeHash(
  documentText: string,
  assets: ReadonlyMap<string, Uint8Array>,
): Promise<string> {
  const parts: Uint8Array[] = [encoder.encode(documentText)];
  for (const path of [...assets.keys()].sort()) {
    parts.push(encoder.encode(path), assets.get(path)!);
  }
  const total = parts.reduce((n, p) => n + 4 + p.length, 0);
  const buffer = new Uint8Array(total);
  const view = new DataView(buffer.buffer);
  let offset = 0;
  for (const part of parts) {
    view.setUint32(offset, part.length);
    buffer.set(part, offset + 4);
    offset += 4 + part.length;
  }
  const digest = await globalThis.crypto.subtle.digest('SHA-256', buffer);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}
