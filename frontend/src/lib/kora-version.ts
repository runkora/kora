/**
 * The kora release this process is, for the theme's version check.
 *
 * Returns the version without the `v`, or null when the build is not from a
 * release tag.
 *
 * A release build carries a tag such as `v0.8.0` or `v0.8.0-rc.2` in
 * `NEXT_PUBLIC_ORBIT_VERSION` (the `ORBIT_VERSION` build argument). A
 * development, local cluster or branch build carries `dev`, a commit hash or
 * nothing, so there is no release to compare a theme with.
 *
 * It imports nothing server-only, so the init container's script can use it.
 */
export function instanceKoraVersion(
  raw: string | undefined = process.env.NEXT_PUBLIC_ORBIT_VERSION,
): string | null {
  const match = /^v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$/.exec(raw?.trim() ?? '');
  return match ? match[1]! : null;
}
