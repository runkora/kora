'use client';

/**
 * Hades – TableColumnHeader
 *
 * The single opinionated way to add sort and/or filter controls to a table
 * column. Implements the rule: all sorts and filters live in the column header
 * — never in a separate filter row or toolbar dropdown.
 *
 * Sort only:
 *   <TableColumnHeader sortDir={dir} onSort={() => cycleSort('name')}>Name</TableColumnHeader>
 *
 * Filter only:
 *   <TableColumnHeader filter={{ options, selected, onToggle, onClear }}>Provider</TableColumnHeader>
 *
 * Sort + filter:
 *   <TableColumnHeader sortDir={dir} onSort={...} filter={{ options, selected, onToggle, onClear }}>
 *     Date
 *   </TableColumnHeader>
 *
 * Static (no interactions) — just use the base `TableHead` from @clear-route/hades instead.
 */

import * as React from 'react';
import { ArrowDown, ArrowUp, ChevronsUpDown, Filter } from 'lucide-react';
import { cn } from '../../utils';
import { Popover, PopoverContent, PopoverTrigger } from '../popover';
import type { SortDir } from './table';

// ─── Filter types ──────────────────────────────────────────────────────────────

/** A single option displayed inside a column filter popover. */
export interface TableColumnFilterOption {
  value: string;
  label: string;
  /**
   * Optional color-dot indicator rendered before the label.
   * Pass a Tailwind background-color class, e.g. `'bg-status-healthy'`.
   */
  dot?: string;
}

/**
 * Structured filter configuration passed to `TableColumnHeader`.
 * The component derives "filter active" state from `selected.size > 0`.
 */
export interface TableColumnFilter {
  /** Options to display in the filter popover. */
  options: TableColumnFilterOption[];
  /** Currently selected values — drives checkmarks and the active-filter highlight. */
  selected: Set<string>;
  /** Called with the option value when the user clicks it. */
  onToggle: (value: string) => void;
  /** Called when the user clicks "Clear filter". Omit to hide the clear button. */
  onClear?: () => void;
}

// ─── Props ────────────────────────────────────────────────────────────────────

export interface TableColumnHeaderProps {
  children: React.ReactNode;
  /** Horizontal alignment of the header content. Defaults to 'left'. */
  align?: 'left' | 'right' | 'center';
  /** Optional fixed width passed as an inline style. */
  width?: string | number;
  /** @internal Extra classes forwarded to the `<th>` element. Used by DataTable for sticky columns. */
  className?: string;

  /**
   * Current sort direction for this column.
   * Omit `onSort` entirely to render a non-sortable header.
   */
  sortDir?: SortDir;
  /** Called when the user clicks the sort button. Use `nextSortDir` to advance the cycle. */
  onSort?: () => void;

  /**
   * Structured filter configuration. When provided, a filter popover is rendered.
   * Active state is derived automatically from `filter.selected.size > 0`.
   * Omit entirely to render no filter affordance on this column.
   */
  filter?: TableColumnFilter;
}

// ─── Sort icon ────────────────────────────────────────────────────────────────

function SortIcon({ dir }: { dir: SortDir }) {
  if (dir === 'asc') return <ArrowUp className="h-3.5 w-3.5 shrink-0" />;
  if (dir === 'desc') return <ArrowDown className="h-3.5 w-3.5 shrink-0" />;
  return (
    <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 opacity-40 group-hover/sort:opacity-70 transition-opacity" />
  );
}

// ─── Filter menu ──────────────────────────────────────────────────────────────

function FilterMenu({ filter }: { filter: TableColumnFilter }) {
  const { options, selected, onToggle, onClear } = filter;
  return (
    <menu className="w-48 p-1 list-none m-0 space-y-0.5">
      {options.map((opt) => (
        <li key={opt.value}>
          <button
            type="button"
            onClick={() => onToggle(opt.value)}
            className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-xs text-foreground transition-colors hover:bg-surface-elevated"
          >
            <span className="flex size-3.5 shrink-0 items-center justify-center rounded-sm border border-border">
              {selected.has(opt.value) && <span className="size-2 rounded-sm bg-accent" />}
            </span>
            {opt.dot && <span className={cn('size-1.5 shrink-0 rounded-full', opt.dot)} />}
            {opt.label}
          </button>
        </li>
      ))}
      {selected.size > 0 && onClear && (
        <>
          <hr className="my-1 border-border" />
          <li>
            <button
              type="button"
              onClick={onClear}
              className="flex w-full items-center rounded-sm px-2 py-1.5 text-xs text-subtle transition-colors hover:bg-surface-elevated hover:text-foreground"
            >
              Clear filter
            </button>
          </li>
        </>
      )}
    </menu>
  );
}

// ─── Component ────────────────────────────────────────────────────────────────

/**
 * TableColumnHeader
 *
 * Renders a `<th>` that can optionally carry a sort button, a filter popover,
 * or both. Matches the visual style of `TableHead` but adds interactive
 * affordances in a consistent, opinionated way.
 *
 * For static (non-interactive) columns use `TableHead` directly.
 */
export function TableColumnHeader({
  children,
  align = 'left',
  width,
  className,
  sortDir,
  onSort,
  filter,
}: TableColumnHeaderProps) {
  const isSortable = onSort !== undefined;
  const isFilterable = filter !== undefined;
  const filterActive = isFilterable && filter.selected.size > 0;

  const alignClass =
    align === 'right'
      ? 'justify-end text-right'
      : align === 'center'
        ? 'justify-center text-center'
        : 'justify-start text-left';

  return (
    <th
      className={cn(
        'h-10 px-4 align-middle text-xs font-medium uppercase tracking-wider text-subtle whitespace-nowrap',
        className,
      )}
      style={width !== undefined ? { width } : undefined}
    >
      <div className={cn('flex items-center gap-1', alignClass)}>
        {/* Label — wrapped in a sort button when sortable */}
        {isSortable ? (
          <button
            type="button"
            onClick={onSort}
            className="group/sort inline-flex items-center gap-1 rounded-sm [font:inherit] [letter-spacing:inherit] [text-transform:inherit] transition-colors hover:text-foreground focus:outline-none focus:ring-1 focus:ring-accent"
          >
            {children}
            <SortIcon dir={sortDir ?? null} />
          </button>
        ) : (
          children
        )}

        {/* Filter popover — only rendered when filter is supplied */}
        {isFilterable && (
          <Popover>
            <PopoverTrigger asChild>
              <button
                type="button"
                className={cn(
                  'rounded p-0.5 transition-colors focus:outline-none focus:ring-1 focus:ring-accent',
                  filterActive ? 'text-accent' : 'text-subtle/70 hover:text-foreground',
                )}
                aria-label="Filter"
              >
                <Filter className="h-3.5 w-3.5" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="start">
              <FilterMenu filter={filter} />
            </PopoverContent>
          </Popover>
        )}
      </div>
    </th>
  );
}
