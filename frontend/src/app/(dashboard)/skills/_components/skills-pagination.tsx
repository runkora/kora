'use client';

/** @tier 3 — skills-scoped. The result range and numbered page controls. */

import { Button, Icon, Stack, Text } from '@runkora/hades';
import { useSkillsContext } from '../_context/skills-context';

/** Page buttons shown on each side of the current page. */
const PAGE_WINDOW = 2;

export function SkillsPagination() {
  const { browse, skills, total, pageSize, status, goToPage } = useSkillsContext();
  const { page } = browse;
  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  const first = skills.length === 0 ? 0 : (page - 1) * pageSize + 1;
  const last = skills.length === 0 ? 0 : first + skills.length - 1;
  const loading = status === 'loading';

  return (
    <Stack direction="horizontal" align="center" justify="between" wrap gap="comfortable">
      <Text as="span" size="caption" color="subtle" tabular>
        {first}–{last} of {total}
      </Text>

      <Stack direction="horizontal" align="center" gap="snug" wrap>
        <Button
          variant="outline"
          size="sm"
          disabled={loading || page <= 1}
          onClick={() => goToPage(page - 1)}
        >
          <Icon name="chevron-left" size="2xs" />
          Previous
        </Button>

        {pageItems(page, pageCount).map((item, index) =>
          item === null ? (
            <Text key={`gap-${index}`} as="span" size="caption" color="subtle" aria-hidden>
              …
            </Text>
          ) : (
            <Button
              key={item}
              variant={item === page ? 'secondary' : 'ghost'}
              size="sm"
              disabled={loading}
              aria-current={item === page ? 'page' : undefined}
              aria-label={`Page ${item}`}
              onClick={() => item !== page && goToPage(item)}
            >
              {item}
            </Button>
          ),
        )}

        <Button
          variant="outline"
          size="sm"
          disabled={loading || page >= pageCount}
          onClick={() => goToPage(page + 1)}
        >
          Next
          <Icon name="chevron-right" size="2xs" />
        </Button>
      </Stack>
    </Stack>
  );
}

/** The first, last and nearby pages, with null for each run of hidden pages: 1 … 5 6 7 8 9 … 20. */
function pageItems(page: number, pageCount: number): (number | null)[] {
  const start = Math.max(2, page - PAGE_WINDOW);
  const end = Math.min(pageCount - 1, page + PAGE_WINDOW);
  const items: (number | null)[] = [1];
  if (start > 2) items.push(null);
  for (let n = start; n <= end; n += 1) items.push(n);
  if (end < pageCount - 1) items.push(null);
  if (pageCount > 1) items.push(pageCount);
  return items;
}
