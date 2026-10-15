/**
 * The kora release this process is, for the theme's version comparison.
 *
 * The image carries its release tag as `NEXT_PUBLIC_ORBIT_VERSION` (the
 * `ORBIT_VERSION` build argument). A release build has a version such as
 * `v0.8.0` or `v0.8.0-rc.2`; a development build, a local cluster build or a
 * build from a branch has `dev`, a commit hash or nothing, and has no release
 * to compare a theme with. Returns the version without the `v`, or null when
 * the build is not from a release tag. Kept free of server-only imports so the
 * init container's script can use it.
 */
export function instanceKoraVersion(
  raw: string | undefined = process.env.NEXT_PUBLIC_ORBIT_VERSION,
): string | null {
  const match = /^v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$/.exec(raw?.trim() ?? '');
  return match ? match[1]! : null;
}
