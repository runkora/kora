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

/**
 * A slug field's value as someone types it: lower case, with each run of other
 * characters as one hyphen. Leading and trailing hyphens stay so that a hyphen
 * can be typed, and the slug check reports them if they remain.
 */
export function slugInput(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-');
}
