/** SHA-256 of the document text, as hex. Logged at startup to tell pods' themes apart. */

const encoder = new TextEncoder();

export async function themeHash(documentText: string): Promise<string> {
  const digest = await globalThis.crypto.subtle.digest('SHA-256', encoder.encode(documentText));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}
