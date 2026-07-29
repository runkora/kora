/**
 * @tier 3 — triggers-scoped. Paging arithmetic for the trigger list.
 *
 * Pure so the awkward parts can be checked directly: a page index that outlives
 * the rows it pointed at, and the human-facing "X to Y of Z" being one-based
 * while the slice is not.
 */

/** Page sizes offered in the footer. The first is the default. */
export const PAGE_SIZES = [12, 25, 50];

export const DEFAULT_PAGE_SIZE = PAGE_SIZES[0] as number;

export interface PageWindow {
  /** The page actually shown, clamped into range. */
  page: number;
  pageCount: number;
  /** Slice bounds, zero-based and half-open. */
  start: number;
  end: number;
  /** One-based positions for the footer's count. */
  from: number;
  to: number;
  total: number;
}

/**
 * Resolves a requested page against the rows that actually exist.
 *
 * Clamping rather than trusting the index matters because filtering shrinks the
 * list under a page that was legitimate a moment ago; without this, narrowing a
 * search to two results while on page 3 shows an empty grid that looks broken.
 */
export function pageWindow(total: number, pageSize: number, requestedPage: number): PageWindow {
  const size = Math.max(1, pageSize);
  const pageCount = Math.max(1, Math.ceil(total / size));
  const page = Math.min(Math.max(0, requestedPage), pageCount - 1);
  const start = page * size;
  const end = Math.min(start + size, total);
  return {
    page,
    pageCount,
    start,
    end,
    // An empty list has nothing to number, so it reads "0 of 0" rather than
    // "1 to 0 of 0".
    from: total === 0 ? 0 : start + 1,
    to: end,
    total,
  };
}
