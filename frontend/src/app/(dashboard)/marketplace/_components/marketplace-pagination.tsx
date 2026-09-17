'use client';

/** @tier 3 — marketplace-scoped. Result range, rows-per-page and page controls, URL-driven. */

import Link from 'next/link';
import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { Button, Icon, NativeSelect, Stack, Text } from '@orbit/terra';

const PAGE_SIZES = [12, 24, 48];

interface PaginationProps {
  total: number;
  page: number;
  pageSize: number;
}

export function MarketplacePagination({ total, page, pageSize }: PaginationProps) {
  const pathname = usePathname();
  const router = useRouter();
  const searchParams = useSearchParams();

  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  const first = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const last = Math.min(total, page * pageSize);

  function createPageURL(pageNumber: number) {
    const params = new URLSearchParams(searchParams);
    params.set('page', String(pageNumber));
    return `${pathname}?${params.toString()}`;
  }

  function handlePageSizeChange(size: number) {
    const params = new URLSearchParams(searchParams);
    params.set('pageSize', String(size));
    params.set('page', '1');
    router.replace(`${pathname}?${params.toString()}`);
  }

  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      wrap
      gap="comfortable"
      style={{ padding: '12px 16px', borderTop: '1px solid var(--color-border-subtle)' }}
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
            onChange={(event) => handlePageSizeChange(Number(event.target.value))}
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

      <Stack direction="horizontal" align="center" gap="snug">
        {page <= 1 ? (
          <Button variant="outline" size="sm" disabled>
            <Icon name="chevron-left" size="2xs" />
            Previous
          </Button>
        ) : (
          <Button variant="outline" size="sm" asChild>
            <Link href={createPageURL(page - 1)}>
              <Icon name="chevron-left" size="2xs" />
              Previous
            </Link>
          </Button>
        )}

        {Array.from({ length: pageCount }, (_, index) => index + 1).map((pageNumber) =>
          pageNumber === page ? (
            <Button key={pageNumber} variant="secondary" size="sm" aria-current="page">
              {pageNumber}
            </Button>
          ) : (
            <Button key={pageNumber} variant="ghost" size="sm" asChild>
              <Link href={createPageURL(pageNumber)}>{pageNumber}</Link>
            </Button>
          ),
        )}

        {page >= pageCount ? (
          <Button variant="outline" size="sm" disabled>
            Next
            <Icon name="chevron-right" size="2xs" />
          </Button>
        ) : (
          <Button variant="outline" size="sm" asChild>
            <Link href={createPageURL(page + 1)}>
              Next
              <Icon name="chevron-right" size="2xs" />
            </Link>
          </Button>
        )}
      </Stack>
    </Stack>
  );
}
