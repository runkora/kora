/**
 * The theme hash: SHA-256 over the document text, hex, so the same document
 * always hashes the same and any change to it changes the hash. The instance
 * logs it at startup, which tells a pod's theme from another's at a glance.
 */

const encoder = new TextEncoder();

export async function themeHash(documentText: string): Promise<string> {
  const digest = await globalThis.crypto.subtle.digest('SHA-256', encoder.encode(documentText));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}
