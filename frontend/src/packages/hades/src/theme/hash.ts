/**
 * The theme hash: SHA-256 over the document text, as hex. The instance logs it
 * at startup, so two pods' themes can be told apart at a glance.
 */

const encoder = new TextEncoder();

export async function themeHash(documentText: string): Promise<string> {
  const digest = await globalThis.crypto.subtle.digest('SHA-256', encoder.encode(documentText));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}
