/**
 * @tier 3 · triggers-scoped. The field path grammar the backend checks for
 * templates, filters and a generic source's event type path.
 */

/** Dotted segments of letters, digits and underscores, e.g. `data.issue.title`. */
export const FIELD_PATH = /^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)*$/;
