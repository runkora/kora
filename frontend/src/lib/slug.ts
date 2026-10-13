/**
 * The slug rule shared by agents and environments: lower case, each run of
 * other characters becomes one hyphen, and the ends lose their hyphens. The
 * result is cut to maxLength characters, by default 63, the longest DNS label.
 */
export function deriveSlug(name: string, maxLength = 63): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, maxLength)
    .replace(/-+$/, '');
}

/**
 * A slug field's value as someone types it: lower case, with each run of other
 * characters as one hyphen. Leading and trailing hyphens stay so that a hyphen
 * can be typed, and the slug check reports them if they remain.
 */
export function slugInput(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-');
}
