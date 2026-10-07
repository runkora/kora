/**
 * The slug rule shared by agents and environments: lower case, each run of
 * other characters becomes one hyphen, and the ends lose their hyphens. The
 * agents service applies the same rule to an environment created without a slug.
 */
export function deriveSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}
