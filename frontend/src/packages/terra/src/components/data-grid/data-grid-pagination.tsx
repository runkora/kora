'use client';

import * as React from 'react';
import { Button } from '../button';
import { Icon } from '../icon';
import { NativeSelect } from '../native-select';
import { DataGridFooter } from './data-grid';

export interface DataGridPaginationProps {
  /** First visible row, 1-based. 0 when the list is empty. */
  from: number;
  /** Last visible row, 1-based. */
  to: number;
  /** Total rows across every page. */
  total: number;
  /** Current page, 0-based. */
  page: number;
  /** Total pages. */
  pageCount: number;
  /** Rows per page. */
  pageSize: number;
  /** Offered page sizes, e.g. [12, 25, 50]. */
  pageSizeOptions: readonly number[];
  onPageChange: (page: number) => void;
  /** A size change implies returning to the first page; the consumer owns that. */
  onPageSizeChange: (size: number) => void;
  /** Plural noun for accessible labels, e.g. "triggers". */
  noun?: string;
}

/**
 * DataGridPagination
 *
 * The standard paging footer for a `DataGrid`: the visible range on the left,
 * a rows-per-page select and previous/next controls on the right, all inside
 * a `DataGridFooter` so the frame closes cleanly. Purely presentational: the
 * consumer owns the paging arithmetic (clamping, reset-on-filter) and passes
 * the computed window in.
 *
 * ```tsx
 * <DataGridPagination
 *   from={13} to={24} total={40}
 *   page={1} pageCount={4}
 *   pageSize={12} pageSizeOptions={[12, 25, 50]}
 *   onPageChange={setPage} onPageSizeChange={setSize}
 *   noun="triggers"
 * />
 * ```
 */
const DataGridPagination = React.forwardRef<HTMLDivElement, DataGridPaginationProps>(
  (
    {
      from,
      to,
      total,
      page,
      pageCount,
      pageSize,
      pageSizeOptions,
      onPageChange,
      onPageSizeChange,
      noun = 'rows',
    },
    ref,
  ) => (
    <DataGridFooter ref={ref} data-slot="data-grid-pagination">
      <span className="text-xs whitespace-nowrap text-subtle">
        {from}–{to} of {total}
      </span>
      <span className="grow" />
      <span className="text-xs whitespace-nowrap text-subtle">Rows per page</span>
      <NativeSelect
        aria-label={`${noun} per page`}
        value={String(pageSize)}
        onChange={(e) => onPageSizeChange(Number(e.target.value))}
      >
        {pageSizeOptions.map((size) => (
          <option key={size} value={size}>
            {size}
          </option>
        ))}
      </NativeSelect>
      <Button
        variant="ghost"
        size="icon-sm"
        disabled={page === 0}
        aria-label="Previous page"
        onClick={() => onPageChange(page - 1)}
      >
        <Icon name="chevron-left" size="2xs" />
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        disabled={page >= pageCount - 1}
        aria-label="Next page"
        onClick={() => onPageChange(page + 1)}
      >
        <Icon name="chevron-right" size="2xs" />
      </Button>
    </DataGridFooter>
  ),
);
DataGridPagination.displayName = 'DataGridPagination';

export { DataGridPagination };
