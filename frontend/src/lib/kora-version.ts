/**
 * The kora release of this process without the `v`, or null for a non-release build.
 * Reads `NEXT_PUBLIC_KORA_VERSION`. It imports nothing server-only, so the init script can use it.
 */
export function instanceKoraVersion(
  raw: string | undefined = process.env.NEXT_PUBLIC_KORA_VERSION,
): string | null {
  const match = /^v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$/.exec(raw?.trim() ?? '');
  return match ? match[1]! : null;
}
