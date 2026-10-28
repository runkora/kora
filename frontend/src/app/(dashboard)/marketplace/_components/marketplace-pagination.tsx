'use client';

/** @tier 3 — marketplace-scoped. Result range, rows-per-page and page controls. */

import { Button, Icon, NativeSelect, Separator, Stack, Text } from '@orbit/hades';
import { useMarketplaceContext } from '../_context/marketplace-context';
import { PAGE_SIZES } from '../_types';

/** The number of page buttons on each side of the current page. */
const PAGE_WINDOW = 2;

/** A page number, or a gap that stands for hidden pages. */
type PageItem = number | 'gap-before' | 'gap-after';

export function MarketplacePagination() {
  const { browse, total, goToPage, setPageSize } = useMarketplaceContext();
  const { page, pageSize } = browse;

  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  let first: number;
  if (total === 0) {
    // No results. Show "0–0 of 0".
    first = 0;
  } else {
    // The number of the first result on this page.
    first = (page - 1) * pageSize + 1;
  }
  const last = Math.min(total, page * pageSize);

  return (
    <Stack gap="none">
      <Separator />
      <Stack
        direction="horizontal"
        align="center"
        justify="between"
        wrap
        gap="comfortable"
        paddingBlock="cozy"
        paddingInline="comfortable"
      >
        <Stack direction="horizontal" align="center" gap="comfortable">
          <Text as="span" size="caption" color="subtle" tabular>
            {first}–{last} of {total}
          </Text>
          <Stack direction="horizontal" align="center" gap="snug">
            <Text as="span" size="caption" color="subtle">
              Rows
            </Text>
            <NativeSelect
              value={String(pageSize)}
              onChange={(event) => setPageSize(Number(event.target.value))}
              aria-label="Results per page"
            >
              {PAGE_SIZES.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </NativeSelect>
          </Stack>
        </Stack>

        <Stack direction="horizontal" align="center" gap="snug" wrap maxWidth="full">
          <Button
            variant="outline"
            size="sm"
            disabled={page <= 1}
            onClick={() => goToPage(page - 1)}
          >
            <Icon name="chevron-left" size="2xs" />
            Previous
          </Button>

          {pageItems(page, pageCount).map((item) => {
            if (item === 'gap-before' || item === 'gap-after') {
              return (
                <Text key={item} as="span" size="caption" color="subtle" aria-hidden>
                  …
                </Text>
              );
            }
            if (item === page) {
              return (
                <Button
                  key={item}
                  variant="secondary"
                  size="sm"
                  aria-current="page"
                  aria-label={`Page ${item}`}
                >
                  {item}
                </Button>
              );
            }
            return (
              <Button
                key={item}
                variant="ghost"
                size="sm"
                onClick={() => goToPage(item)}
                aria-label={`Page ${item}`}
              >
                {item}
              </Button>
            );
          })}

          <Button
            variant="outline"
            size="sm"
            disabled={page >= pageCount}
            onClick={() => goToPage(page + 1)}
          >
            Next
            <Icon name="chevron-right" size="2xs" />
          </Button>
        </Stack>
      </Stack>
    </Stack>
  );
}

/**
 * Returns the page buttons to show: the first page, the last page, and the
 * pages within PAGE_WINDOW of the current page. A gap stands for each run of
 * hidden pages. For example, page 7 of 20 gives 1 … 5 6 7 8 9 … 20.
 */
function pageItems(page: number, pageCount: number): PageItem[] {
  const items: PageItem[] = [1];
  const start = Math.max(2, page - PAGE_WINDOW);
  const end = Math.min(pageCount - 1, page + PAGE_WINDOW);

  if (start > 2) {
    items.push('gap-before');
  }
  for (let pageNumber = start; pageNumber <= end; pageNumber += 1) {
    items.push(pageNumber);
  }
  if (end < pageCount - 1) {
    items.push('gap-after');
  }
  if (pageCount > 1) {
    items.push(pageCount);
  }
  return items;
}
