'use client';

/**
 * @tier 1 — Zero Orbit domain knowledge. Could be extracted to a standalone library.
 *
 * Terra – DataTable
 *
 * The opinionated high-level table component. CSS grid layout (no `<table>`
 * elements), matching the visual language of the former DataGrid primitive.
 * Encapsulates search, sort, client/server-side filtering, column visibility,
 * and optional client-side pagination — all driven from a column-definition
 * array and a dataset. Built on @tanstack/react-table internally; consumers
 * never import TanStack directly.
 *
 * ## DataTable vs SmartTable decision tree
 *
 * ```
 * Are you building inside (dashboard)/ (authenticated shell)?
 * ├── No  → use DataTable from @orbit/terra
 * └── Yes → Does this table need column visibility persisted per user?
 *           ├── No  → use DataTable from @orbit/terra
 *           └── Yes → use SmartTable from @orbit/components  ← default choice
 * ```
 *
 * `SmartTable` is the default for the dashboard. Reach for `DataTable` directly
 * only when the component renders outside `(dashboard)/layout.tsx` (no
 * PreferencesProvider), column visibility is managed externally, or the table
 * has no toggleable columns.
 *
 * ## Per-row derived values pattern
 *
 * When multiple cells in a row depend on the same derived computation, define a
 * view-model type that carries the pre-computed fields, transform the raw data
 * in a `useMemo`, and use the enriched type as `TData`.
 *
 * ```tsx
 * interface DisplayRow extends RawRow {
 *   failures: number
 *   activity: number[]
 * }
 * const rows = useMemo<DisplayRow[]>(() => (
 *   raw.map(r => ({ ...r, failures: computeFailures(r), activity: computeActivity(r) }))
 * ), [raw])
 * <DataTable data={rows} columns={columns} getRowKey={(r) => r.id} />
 * ```
 *
 * ## Pagination patterns
 *
 * **Client-side** — pass the `pagination` prop; DataTable owns all page state:
 * ```tsx
 * <DataTable data={allRows} columns={columns} getRowKey={(r) => r.id}
 *   pagination={{ pageSizeOptions: [10, 25, 50], defaultPageSize: 25 }}
 *   noun="triggers" />
 * ```
 *
 * **Server-side** — manage page state externally, pass the current page slice
 * as `data`. Render your own pagination controls beneath the table.
 *
 * **Server-side sort** — provide `onSortChange`; DataTable delegates reordering
 * to the consumer and disables client-side sort.
 */

import * as React from 'react';
import {
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  type ColumnDef,
  type SortingState,
  type VisibilityState,
} from '@tanstack/react-table';
import { cn } from '../../utils';
import { sectionLabelVariants } from '../section-label';
import { Popover, PopoverContent, PopoverTrigger } from '../popover';
import { Button } from '../button';
import { Icon } from '../icon';
import { NativeSelect } from '../native-select';
import { Tooltip, TooltipTrigger, TooltipContent } from '../tooltip';

// ─── Filter types ──────────────────────────────────────────────────────────────

/**
 * A single option displayed inside a column filter popover.
 */
export interface DataTableFilterOption {
  value: string;
  label: string;
  /**
   * Optional color-dot rendered before the label.
   * Pass a Tailwind background-color class, e.g. `'bg-status-healthy'`.
   */
  dot?: string;
}

/**
 * Client-side column filter. Terra owns the `selected` Set. Rows are
 * pre-filtered in-component. No external state management required.
 */
export interface DataTableClientFilter<TData> {
  mode: 'client';
  options: DataTableFilterOption[];
  /**
   * Render a search box and a scrollable list in the filter popover. Use for
   * filters with many options (e.g. scopes). Selection is multi-select; how
   * multiple selections combine is up to `filterFn` (union = `some`).
   */
  searchable?: boolean;
  /** Render option labels in a monospaced face (scope strings, IDs). */
  mono?: boolean;
  /**
   * Return true if the row should be included given the active selections.
   * Only called when `selected.size > 0`.
   */
  filterFn: (row: TData, selected: Set<string>) => boolean;
}

/**
 * Server-side column filter. The consumer owns the selected state and
 * triggers a new data fetch when selection changes.
 */
export interface DataTableServerFilter {
  mode: 'server';
  options: DataTableFilterOption[];
  /** See `DataTableClientFilter.searchable`. */
  searchable?: boolean;
  /** See `DataTableClientFilter.mono`. */
  mono?: boolean;
  /** Currently selected values — managed externally by the consumer. */
  selected: Set<string>;
  onToggle: (value: string) => void;
  /** Called when the user clears the filter. Omit to hide the clear button. */
  onClear?: () => void;
}

/** Union of client-managed and server-managed filter modes. */
export type DataTableColumnFilter<TData> = DataTableClientFilter<TData> | DataTableServerFilter;

// ─── Column definition ─────────────────────────────────────────────────────────

export interface DataTableColumn<TData> {
  /** Unique stable identifier. Used as a React key and in visibility state. */
  id: string;
  /** Column header label. */
  header: string;
  /** Optional tooltip shown on an info icon next to the header label. */
  headerTooltip?: string;
  /**
   * CSS grid column sizing value for this column.
   * Examples: '1fr', '168px', 'minmax(0, 1.1fr)', '74px'
   * Values are assembled into the grid-template-columns string in column order.
   * Defaults to '1fr' when omitted.
   */
  gridWidth?: string;
  /**
   * Minimum width of this column's grid track, as a CSS length (e.g. '8rem').
   * Flexible columns (`fr` or `minmax(0, …fr)`) never shrink below it, so
   * content is clipped with an ellipsis instead of collapsing the column.
   * Defaults to `DEFAULT_COLUMN_MIN_WIDTH` for flexible columns. Fixed-length
   * `gridWidth` values are left as-is unless `minWidth` is set explicitly.
   */
  minWidth?: string;
  /**
   * Let this column's cell content wrap onto multiple lines instead of being
   * clipped with an ellipsis. Defaults to false.
   */
  wrap?: boolean;
  /** Renders the cell content for a given data row. */
  cell: (row: TData) => React.ReactNode;
  /** Whether a sort button is rendered on this column. Defaults to false. */
  sortable?: boolean;
  /**
   * Extract the primitive value used for sort comparisons.
   * Required when `sortable` is true.
   */
  sortAccessor?: (row: TData) => string | number;
  /** Initial column visibility. Defaults to true. */
  defaultVisible?: boolean;
  /**
   * Locked columns are always visible and excluded from the column visibility
   * toggle. The first locked column is pinned sticky-left when the table
   * scrolls horizontally.
   */
  locked?: boolean;
  /**
   * Pins this column to the right edge when the table scrolls horizontally, so
   * the other columns scroll underneath it and it is never pushed off screen.
   * Use it for the row-actions column. Only the first column with `pinEnd` is
   * pinned.
   */
  pinEnd?: boolean;
  /** Header and cell text alignment. Defaults to 'left'. */
  align?: 'left' | 'right' | 'center';
  /** Column-level filter. Omit for non-filterable columns. */
  filter?: DataTableColumnFilter<TData>;
}

// ─── Table props ───────────────────────────────────────────────────────────────

export interface DataTableProps<TData> {
  data: TData[];
  columns: DataTableColumn<TData>[];
  getRowKey: (row: TData) => string;

  // Row interaction
  onRowClick?: (row: TData) => void;
  /**
   * Per-row accessible label announced by screen readers when a clickable row
   * receives focus. Required when `onRowClick` is provided.
   * Example: `rowLabel={(row) => \`Open ${row.name}\`}`
   */
  rowLabel?: (row: TData) => string;

  // Search
  searchPlaceholder?: string;
  searchFn?: (row: TData, query: string) => boolean;

  // Sort
  defaultSortId?: string;
  defaultSortDir?: 'asc' | 'desc';
  /**
   * When provided, disables client-side row reordering. The consumer is
   * responsible for refetching with the new sort params.
   */
  onSortChange?: (columnId: string, dir: 'asc' | 'desc') => void;

  // Column visibility (hook-in for external persistence; used by SmartTable)
  initialColumnVisibility?: Record<string, boolean>;
  onColumnVisibilityChange?: (id: string, visible: boolean) => void;

  // Toolbar
  toolbarActions?: React.ReactNode;
  /** Rendered left of the centered count — use for a section title. */
  toolbarTitle?: React.ReactNode;
  /** Rendered right of center, before the columns button and search. */
  quickFilters?: React.ReactNode;

  // Empty / error states
  emptyMessage?: string;
  filteredEmptyMessage?: string;
  error?: React.ReactNode;

  // Pagination noun (shown in footer)
  noun?: string;

  /**
   * When provided, enables client-side pagination via an internal footer.
   * For server-side pagination, manage page state externally and pass the
   * current page's slice as `data`.
   */
  pagination?: {
    pageSizeOptions: readonly number[];
    defaultPageSize?: number;
  };
}

// ─── Internal context ──────────────────────────────────────────────────────────

/** Floor for flexible columns that do not declare their own `minWidth`. */
const DEFAULT_COLUMN_MIN_WIDTH = '5rem';

const MINMAX_RE = /^minmax\(\s*([^,]+?)\s*,\s*(.+)\s*\)$/;

/**
 * Resolves a column to its `grid-template-columns` track. A bare `1fr` track
 * has an automatic minimum of zero here (cells are `min-w-0`), so without a
 * floor a flexible column collapses to a few pixels when its neighbours are
 * wide.
 */
function gridTrack<TData>(col: DataTableColumn<TData>): string {
  const width = col.gridWidth ?? '1fr';
  const minmax = MINMAX_RE.exec(width);
  if (minmax) {
    const [, min, max] = minmax;
    const floor = col.minWidth ?? (parseFloat(min) === 0 ? DEFAULT_COLUMN_MIN_WIDTH : min);
    return `minmax(${floor}, ${max})`;
  }
  if (width.endsWith('fr')) return `minmax(${col.minWidth ?? DEFAULT_COLUMN_MIN_WIDTH}, ${width})`;
  return col.minWidth ? `minmax(${col.minWidth}, ${width})` : width;
}

// Clip overflowing content to the cell. Ellipsis only applies to text that is
// a direct inline child of the clipping box, so the same treatment is applied
// to the cell's children and grandchildren (a value in a Stack, a chip in a
// wrapping list).
const CELL_CLIP =
  'overflow-hidden text-ellipsis whitespace-nowrap ' +
  '[&>*]:max-w-full [&>*]:overflow-hidden [&>*]:text-ellipsis ' +
  '[&>*>*]:max-w-full [&>*>*]:overflow-hidden [&>*>*]:text-ellipsis';

/**
 * Cell wrapper: clips overflow (or wraps, when `wrap`) and, on hover, sets a
 * native `title` with the full text when something is actually truncated.
 */
function DataTableCell({
  wrap,
  className,
  children,
}: {
  wrap?: boolean;
  className?: string;
  children: React.ReactNode;
}) {
  const ref = React.useRef<HTMLDivElement>(null);

  const handleMouseEnter = () => {
    const el = ref.current;
    if (!el || wrap) return;
    const truncated = [el, ...Array.from(el.querySelectorAll<HTMLElement>('*'))].some(
      (n) => n.scrollWidth > n.clientWidth + 1,
    );
    const text = el.textContent?.trim();
    if (truncated && text) el.title = text;
    else el.removeAttribute('title');
  };

  return (
    <div
      ref={ref}
      role="cell"
      onMouseEnter={handleMouseEnter}
      className={cn('min-w-0', wrap ? 'whitespace-normal' : CELL_CLIP, className)}
    >
      {children}
    </div>
  );
}

// A pinned cell must be solid across the whole row, or scrolled columns show
// through around its content. Rows centre their cells, so a pinned cell would
// otherwise be only as tall as its content. These classes stretch it to the row's
// full height, cancel the row's vertical padding with negative margins and
// matching padding, and re-centre the content. Horizontal padding is handled
// per side (`-ml-4 pl-4` / `-mr-4 pr-4`) at the use site.
const PINNED_FILL_BODY = 'z-10 -my-3 flex items-center self-stretch py-3 transition-colors';
const PINNED_FILL_HEADER = 'z-10 -my-[9px] flex items-center self-stretch py-[9px]';

/**
 * The edge and background of a pinned cell. It always reserves a transparent
 * 1px border, so nothing shifts when it appears, and it only gets a line and
 * elevation while scrolled content is actually sitting underneath it
 * (`overlapping`).
 *
 * Elevation reads differently per theme. Dark is the default theme, where a
 * shadow on a near-black surface is barely visible, so it also takes the
 * `surface-elevated` tone and a stronger shadow. Light overrides both with the
 * plain surface and a much softer shadow. The line is the same subtle colour as
 * the row dividers in both.
 */
function pinnedEdge(side: 'start' | 'end', overlapping: boolean): string {
  const border = side === 'start' ? 'border-r' : 'border-l';
  if (!overlapping) return cn(border, 'border-transparent bg-surface');
  return side === 'start'
    ? `${border} border-border-subtle bg-surface-elevated shadow-[8px_0_12px_-6px_rgb(0_0_0/0.8)] [[data-theme=light]_&]:bg-surface [[data-theme=light]_&]:shadow-[6px_0_8px_-6px_rgb(0_0_0/0.15)]`
    : `${border} border-border-subtle bg-surface-elevated shadow-[-8px_0_12px_-6px_rgb(0_0_0/0.8)] [[data-theme=light]_&]:bg-surface [[data-theme=light]_&]:shadow-[-6px_0_8px_-6px_rgb(0_0_0/0.15)]`;
}

const COLUMN_GAP_PX = 14;
const ROW_PADDING_PX = 32; // 16px on each side of a row

/**
 * The narrowest a column's track can get, as a CSS length. Anything that is
 * not a `px` or `rem` length (for example `auto`) counts as zero.
 */
function minTrackLength<TData>(col: DataTableColumn<TData>): string {
  const track = gridTrack(col);
  const minmax = MINMAX_RE.exec(track);
  const min = (minmax ? minmax[1] : track).trim();
  return /^[\d.]+(rem|px)$/.test(min) ? min : '0px';
}

/**
 * The width below which the columns cannot shrink any further: every column at
 * its minimum, plus the gaps and the row padding. The table body scrolls
 * horizontally only once its container is narrower than this, so scrolling is
 * a last resort after every column has hit its minimum.
 */
export function tableMinWidth<TData>(columns: readonly DataTableColumn<TData>[]): string {
  const fixed = COLUMN_GAP_PX * Math.max(columns.length - 1, 0) + ROW_PADDING_PX;
  return `calc(${[...columns.map(minTrackLength), `${fixed}px`].join(' + ')})`;
}

interface DataTableContext {
  gridTemplateColumns: string;
}

const DataTableCtx = React.createContext<DataTableContext>({
  gridTemplateColumns: '1fr',
});

// ─── Component ─────────────────────────────────────────────────────────────────

export function DataTable<TData>({
  data,
  columns: columnDefs,
  getRowKey,
  onRowClick,
  rowLabel,
  searchPlaceholder,
  searchFn,
  defaultSortId,
  defaultSortDir = 'asc',
  onSortChange,
  initialColumnVisibility,
  onColumnVisibilityChange,
  toolbarActions,
  toolbarTitle,
  quickFilters,
  emptyMessage = 'No records found.',
  filteredEmptyMessage = 'No records match your filters.',
  error,
  noun = 'records',
  pagination,
}: DataTableProps<TData>) {
  // ── State ───────────────────────────────────────────────────────────────────

  const [searchQuery, setSearchQuery] = React.useState('');

  const [clientFilters, setClientFilters] = React.useState<Record<string, Set<string>>>({});

  const [sorting, setSorting] = React.useState<SortingState>(() =>
    defaultSortId ? [{ id: defaultSortId, desc: defaultSortDir === 'desc' }] : [],
  );

  const [columnVisibility, setColumnVisibility] = React.useState<VisibilityState>(() => {
    const vis: VisibilityState = {};
    columnDefs.forEach((col) => {
      if (!col.locked) {
        if (initialColumnVisibility && col.id in initialColumnVisibility) {
          vis[col.id] = initialColumnVisibility[col.id];
        } else {
          vis[col.id] = col.defaultVisible ?? true;
        }
      }
    });
    return vis;
  });

  // Client-side pagination state (only when pagination prop is provided)
  const [page, setPage] = React.useState(0);
  const [pageSize, setPageSize] = React.useState(
    pagination?.defaultPageSize ?? pagination?.pageSizeOptions[0] ?? 25,
  );

  // ── Pre-filter (search + client-side column filters) ────────────────────────

  const filteredData = React.useMemo(() => {
    let result = data;

    if (searchFn && searchQuery.trim()) {
      const q = searchQuery.trim().toLowerCase();
      result = result.filter((row) => searchFn(row, q));
    }

    for (const col of columnDefs) {
      if (col.filter?.mode === 'client') {
        const selected = clientFilters[col.id];
        if (selected && selected.size > 0) {
          const { filterFn } = col.filter as DataTableClientFilter<TData>;
          result = result.filter((row) => filterFn(row, selected));
        }
      }
    }

    return result;
  }, [data, searchQuery, searchFn, clientFilters, columnDefs]);

  // ── TanStack column definitions ─────────────────────────────────────────────

  const tanstackColumns = React.useMemo<ColumnDef<TData, unknown>[]>(
    () =>
      columnDefs.map((col) => ({
        id: col.id,
        accessorFn:
          col.sortable && col.sortAccessor
            ? (row: TData) => (col.sortAccessor as (r: TData) => string | number)(row)
            : () => null,
        header: col.header,
        cell: ({ row }) => col.cell(row.original),
        enableSorting: col.sortable ?? false,
        enableHiding: !(col.locked ?? false),
        sortingFn: 'alphanumeric' as const,
      })),
    [columnDefs],
  );

  // ── Stable row model factories ──────────────────────────────────────────────

  const coreRowModelRef = React.useRef<ReturnType<typeof getCoreRowModel> | null>(null);
  if (!coreRowModelRef.current) coreRowModelRef.current = getCoreRowModel();
  const sortedRowModelRef = React.useRef<ReturnType<typeof getSortedRowModel> | null>(null);
  if (!sortedRowModelRef.current) sortedRowModelRef.current = getSortedRowModel();

  // ── TanStack table instance ─────────────────────────────────────────────────
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: filteredData,
    columns: tanstackColumns,
    state: { sorting, columnVisibility },
    onSortingChange: setSorting,
    onColumnVisibilityChange: setColumnVisibility,
    getCoreRowModel: coreRowModelRef.current,
    getSortedRowModel: sortedRowModelRef.current,
    manualFiltering: true,
    manualSorting: onSortChange !== undefined,
    enableSortingRemoval: false,
  });

  // ── Sort cycling: asc ↔ desc ───────────────────────────────────────────────

  const handleSort = React.useCallback(
    (columnId: string) => {
      const existing = sorting.find((s) => s.id === columnId);
      const next: SortingState = existing
        ? [{ id: columnId, desc: !existing.desc }]
        : [{ id: columnId, desc: false }];
      setSorting(next);
      onSortChange?.(next[0].id, next[0].desc ? 'desc' : 'asc');
    },
    [sorting, setSorting, onSortChange],
  );

  // ── Client filter handlers ──────────────────────────────────────────────────

  function toggleClientFilter(columnId: string, value: string) {
    setClientFilters((prev) => {
      const current = prev[columnId] ?? new Set<string>();
      const next = new Set(current);
      if (next.has(value)) next.delete(value);
      else next.add(value);
      return { ...prev, [columnId]: next };
    });
  }

  function clearClientFilter(columnId: string) {
    setClientFilters((prev) => {
      const next = { ...prev };
      delete next[columnId];
      return next;
    });
  }

  // ── Pagination handlers ─────────────────────────────────────────────────────

  function handlePageChange(nextPage: number) {
    setPage(nextPage);
  }

  function handlePageSizeChange(nextSize: number) {
    setPageSize(nextSize);
    setPage(0);
  }

  // Reset page to 0 when search query or client-side column filters change.
  // Serialise Set values by sorting their entries so the key changes reliably
  // when selections toggle (JSON.stringify renders Sets as `{}` always).
  const filterKey = React.useMemo(() => {
    const setEntries = Object.entries(clientFilters)
      .map(([colId, sel]) => `${colId}:${[...sel].sort().join(',')}`)
      .join('|');
    return `${searchQuery}||${setEntries}`;
  }, [searchQuery, clientFilters]);

  const prevFilterKeyRef = React.useRef(filterKey);
  React.useEffect(() => {
    if (filterKey !== prevFilterKeyRef.current) {
      prevFilterKeyRef.current = filterKey;
      setPage(0);
    }
  }, [filterKey]);

  // ── Derived row data ────────────────────────────────────────────────────────

  const allRows = table.getRowModel().rows;

  // Clamp page to valid range when data shrinks (e.g. a prop-driven filter
  // upstream reduces the row count, making the current page out of range).
  // Use a layout effect so we never call setState during render.
  const safePageCount = pagination ? Math.max(1, Math.ceil(allRows.length / pageSize)) : 1;
  const safePage = Math.min(page, safePageCount - 1);
  React.useLayoutEffect(() => {
    if (safePage !== page) setPage(safePage);
  }, [safePage, page]);

  const paginatedRows = React.useMemo(() => {
    if (!pagination) return allRows;
    const start = safePage * pageSize;
    return allRows.slice(start, start + pageSize);
  }, [allRows, pagination, safePage, pageSize]);

  const visibleColDefs = columnDefs.filter((col) => {
    if (col.locked) return true;
    return columnVisibility[col.id] !== false;
  });

  // ── Grid context value ──────────────────────────────────────────────────────

  const gridTemplateColumns = visibleColDefs.map(gridTrack).join(' ');

  // ── Derived display state ───────────────────────────────────────────────────

  const hasActiveFilters =
    searchQuery.trim().length > 0 ||
    Object.values(clientFilters).some((s) => s.size > 0) ||
    columnDefs.some(
      (c) => c.filter?.mode === 'server' && (c.filter as DataTableServerFilter).selected.size > 0,
    );

  const pinnedColId = columnDefs.find((c) => c.locked)?.id;
  const pinnedEndColId = visibleColDefs.find((c) => c.pinEnd)?.id;

  // Whether scrolled columns are currently sitting under a pinned column: the
  // left one once scrolled away from the start, the right one while there is
  // more to the right. Only then do pinned cells get an edge and shadow.
  const scrollRef = React.useRef<HTMLDivElement>(null);
  const [overlap, setOverlap] = React.useState({ start: false, end: false });
  React.useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const update = () => {
      const start = el.scrollLeft > 1;
      const end = el.scrollLeft + el.clientWidth < el.scrollWidth - 1;
      setOverlap((prev) => (prev.start === start && prev.end === end ? prev : { start, end }));
    };
    el.addEventListener('scroll', update, { passive: true });
    // The observer's first callback computes the initial state, and it fires
    // again whenever the viewport or the set of columns changes the content width.
    let observer: ResizeObserver | undefined;
    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(update);
      observer.observe(el);
      if (el.firstElementChild) observer.observe(el.firstElementChild);
    }
    return () => {
      el.removeEventListener('scroll', update);
      observer?.disconnect();
    };
  }, []);

  const toolbarColumnItems = columnDefs
    .filter((c) => !c.locked)
    .map((c) => ({
      id: c.id,
      label: c.header,
      visible: columnVisibility[c.id] !== false,
    }));

  // Pagination window
  const pageCount = safePageCount;
  const from = pagination && allRows.length > 0 ? safePage * pageSize + 1 : 1;
  const to = pagination ? Math.min((safePage + 1) * pageSize, allRows.length) : allRows.length;

  // ── Render ──────────────────────────────────────────────────────────────────

  return (
    <DataTableCtx.Provider value={{ gridTemplateColumns }}>
      <div
        role="table"
        aria-label={noun}
        className="overflow-hidden rounded-lg border border-border-subtle bg-surface"
      >
        {/* Toolbar */}
        <DataTableToolbar
          searchQuery={searchQuery}
          onSearchChange={setSearchQuery}
          searchPlaceholder={searchPlaceholder}
          hasSearch={!!searchFn}
          columnItems={toolbarColumnItems}
          onColumnToggle={(id) => {
            const col = table.getColumn(id);
            if (!col) return;
            const newVisible = !col.getIsVisible();
            col.toggleVisibility();
            onColumnVisibilityChange?.(id, newVisible);
          }}
          actions={toolbarActions}
          quickFilters={quickFilters}
          title={toolbarTitle}
        />

        {/* The header and body scroll together, horizontally, and only once the
            container is narrower than every column at its minimum width. The
            toolbar and pagination stay put. */}
        {/* A styled WebKit scrollbar is always drawn, unlike the default overlay
            scrollbar that macOS hides until you scroll. That is what tells the
            user the table scrolls. */}
        <div
          ref={scrollRef}
          className="overflow-x-auto [&::-webkit-scrollbar]:h-2.5 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-border [&::-webkit-scrollbar-thumb:hover]:bg-subtle [&::-webkit-scrollbar-track]:bg-surface-elevated"
        >
          <div style={{ minWidth: tableMinWidth(visibleColDefs) }}>
            {/* Header row */}
            <DataTableHeader
              columnDefs={visibleColDefs}
              allColumnDefs={columnDefs}
              sorting={sorting}
              clientFilters={clientFilters}
              onSort={handleSort}
              onToggleClientFilter={toggleClientFilter}
              onClearClientFilter={clearClientFilter}
              pinnedColId={pinnedColId}
              pinnedEndColId={pinnedEndColId}
              overlap={overlap}
            />

            {/* Body */}
            <div role="rowgroup">
              {error ? (
                <div className="px-4 py-8">{error}</div>
              ) : paginatedRows.length === 0 ? (
                <div className="flex flex-col items-center gap-2 px-5 py-10 text-center">
                  <p className="text-sm text-subtle">
                    {hasActiveFilters ? filteredEmptyMessage : emptyMessage}
                  </p>
                </div>
              ) : (
                paginatedRows.map((row) => {
                  const label = rowLabel ? rowLabel(row.original) : undefined;
                  const clickable = !!onRowClick;
                  return (
                    <div
                      key={getRowKey(row.original)}
                      role="row"
                      aria-label={label}
                      tabIndex={clickable ? 0 : undefined}
                      onClick={clickable ? () => onRowClick(row.original) : undefined}
                      onKeyDown={
                        clickable
                          ? (e) => {
                              if (
                                (e.key === 'Enter' || e.key === ' ') &&
                                e.target === e.currentTarget
                              ) {
                                e.preventDefault();
                                onRowClick(row.original);
                              }
                            }
                          : undefined
                      }
                      className={cn(
                        'group grid items-center border-b border-border-subtle px-4 py-3',
                        // Remove bottom border from the last row (same CSS trick as DataGrid)
                        '[&:not(:has(~[role=row]))]:border-b-0',
                        clickable &&
                          'cursor-pointer transition-colors hover:bg-surface-elevated focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent',
                      )}
                      style={{ gridTemplateColumns, columnGap: '14px' }}
                    >
                      {visibleColDefs.map((colDef) => {
                        const isPinned = colDef.id === pinnedColId;
                        const isPinnedEnd = colDef.id === pinnedEndColId;
                        return (
                          <DataTableCell
                            key={colDef.id}
                            wrap={colDef.wrap}
                            className={cn(
                              colDef.align === 'right' && 'text-right',
                              colDef.align === 'center' && 'text-center',
                              isPinned &&
                                `${PINNED_FILL_BODY} -ml-4 pl-4 sticky left-0 ${pinnedEdge('start', overlap.start)}`,
                              isPinned && clickable && 'group-hover:bg-surface-elevated',
                              isPinnedEnd &&
                                `${PINNED_FILL_BODY} -mr-4 justify-center pl-2 pr-2 sticky right-0 ${pinnedEdge('end', overlap.end)}`,
                              isPinnedEnd && clickable && 'group-hover:bg-surface-elevated',
                            )}
                          >
                            {colDef.cell(row.original)}
                          </DataTableCell>
                        );
                      })}
                    </div>
                  );
                })
              )}
            </div>
          </div>
        </div>

        {/* Pagination footer */}
        {pagination && pageCount > 1 && (
          <DataTablePagination
            from={from}
            to={to}
            total={allRows.length}
            page={safePage}
            pageCount={pageCount}
            pageSize={pageSize}
            pageSizeOptions={pagination.pageSizeOptions}
            onPageChange={handlePageChange}
            onPageSizeChange={handlePageSizeChange}
            noun={noun}
          />
        )}
      </div>
    </DataTableCtx.Provider>
  );
}

// ─── Internal: Toolbar ─────────────────────────────────────────────────────────

interface ToolbarProps {
  searchQuery: string;
  onSearchChange: (v: string) => void;
  searchPlaceholder?: string;
  hasSearch: boolean;
  columnItems: { id: string; label: string; visible: boolean }[];
  onColumnToggle: (id: string) => void;
  actions?: React.ReactNode;
  quickFilters?: React.ReactNode;
  title?: React.ReactNode;
}

function DataTableToolbar({
  searchQuery,
  onSearchChange,
  searchPlaceholder,
  hasSearch,
  columnItems,
  onColumnToggle,
  actions,
  quickFilters,
  title,
}: ToolbarProps) {
  return (
    <div className="flex items-center gap-2 border-b border-border-subtle px-4 py-3 min-h-[52px]">
      {/* Left: optional title, then quick filters, then columns button */}
      <div className="flex items-center gap-2">
        {title}
        {quickFilters}
        {columnItems.length > 0 && (
          <ColumnVisibilityToggle items={columnItems} onToggle={onColumnToggle} />
        )}
      </div>

      {/* Right: search input then action buttons */}
      <div className="ml-auto flex items-center gap-2">
        {hasSearch && (
          <div className="relative">
            <div className="absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none text-subtle">
              <Icon name="search" size="2xs" />
            </div>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => onSearchChange(e.target.value)}
              placeholder={searchPlaceholder ?? 'Search…'}
              className="h-8 w-56 rounded-md border border-border-subtle bg-surface pl-8 pr-3 text-sm text-foreground placeholder:text-subtle focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
        )}
        {actions}
      </div>
    </div>
  );
}

function ColumnVisibilityToggle({
  items,
  onToggle,
}: {
  items: { id: string; label: string; visible: boolean }[];
  onToggle: (id: string) => void;
}) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="Toggle column visibility"
          className="inline-flex h-9 items-center gap-1.5 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground transition-colors hover:bg-surface-elevated focus:outline-none focus:ring-1 focus:ring-accent"
        >
          <Icon name="settings-2" size="sm" />
          Columns
        </button>
      </PopoverTrigger>
      <PopoverContent align="end">
        <menu className="min-w-40 p-1.5 space-y-0.5">
          {items.map((item) => (
            <li key={item.id} className="list-none">
              <button
                type="button"
                onClick={() => onToggle(item.id)}
                className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-foreground transition-colors hover:bg-surface-elevated"
              >
                <span
                  className={cn(
                    'flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border',
                    item.visible
                      ? 'border-accent bg-accent text-accent-foreground'
                      : 'border-border',
                  )}
                >
                  {item.visible && <Icon name="check" size="2xs" />}
                </span>
                {item.label}
              </button>
            </li>
          ))}
        </menu>
      </PopoverContent>
    </Popover>
  );
}

// ─── Internal: Header label ────────────────────────────────────────────────────

function HeaderInfo({ label, tooltip }: { label: string; tooltip: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label={`More information about ${label}`}
          className="inline-flex cursor-default text-subtle hover:text-foreground transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent rounded-sm"
        >
          <Icon name="info" size="2xs" />
        </button>
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}

// The info control is a sibling of the sort button, never a child of it: a
// button inside a button is invalid HTML, and the inner click bubbles into a
// sort the user did not ask for.
function HeaderLabel({ label, tooltip }: { label: string; tooltip?: string }) {
  if (!tooltip) return <>{label}</>;
  return (
    <span className="inline-flex items-center gap-1">
      {label}
      <HeaderInfo label={label} tooltip={tooltip} />
    </span>
  );
}

// ─── Internal: Header row ──────────────────────────────────────────────────────

interface HeaderProps<TData> {
  columnDefs: DataTableColumn<TData>[];
  allColumnDefs: DataTableColumn<TData>[];
  sorting: SortingState;
  clientFilters: Record<string, Set<string>>;
  onSort: (id: string) => void;
  onToggleClientFilter: (colId: string, value: string) => void;
  onClearClientFilter: (colId: string) => void;
  pinnedColId: string | undefined;
  pinnedEndColId: string | undefined;
  overlap: { start: boolean; end: boolean };
}

function DataTableHeader<TData>({
  columnDefs,
  sorting,
  clientFilters,
  onSort,
  onToggleClientFilter,
  onClearClientFilter,
  pinnedColId,
  pinnedEndColId,
  overlap,
}: HeaderProps<TData>) {
  const { gridTemplateColumns } = React.useContext(DataTableCtx);

  return (
    <div
      role="row"
      className={cn(
        'relative grid items-center border-b border-border-subtle px-4 py-[9px]',
        sectionLabelVariants(),
      )}
      style={{ gridTemplateColumns, columnGap: '14px' }}
    >
      {columnDefs.map((col) => {
        const sortState = sorting.find((s) => s.id === col.id);
        const sortDir: 'asc' | 'desc' | null = sortState ? (sortState.desc ? 'desc' : 'asc') : null;

        const clientFilterSelected = clientFilters[col.id] ?? new Set<string>();
        const serverFilter =
          col.filter?.mode === 'server' ? (col.filter as DataTableServerFilter) : undefined;
        const filterSelected = serverFilter ? serverFilter.selected : clientFilterSelected;
        const filterActive = filterSelected.size > 0;

        const isPinned = col.id === pinnedColId;
        const isPinnedEnd = col.id === pinnedEndColId;
        const alignClass =
          col.align === 'right'
            ? 'justify-end text-right'
            : col.align === 'center'
              ? 'justify-center text-center'
              : '';

        const hasSort = col.sortable === true;
        const hasFilter = col.filter !== undefined;

        if (!hasSort && !hasFilter) {
          return (
            <span
              key={col.id}
              role="columnheader"
              className={cn(
                'min-w-0 overflow-hidden break-words',
                alignClass,
                isPinned &&
                  `${PINNED_FILL_HEADER} -ml-4 pl-4 sticky left-0 ${pinnedEdge('start', overlap.start)}`,
                isPinnedEnd &&
                  `${PINNED_FILL_HEADER} -mr-4 justify-center pl-2 pr-2 sticky right-0 ${pinnedEdge('end', overlap.end)}`,
              )}
            >
              <HeaderLabel label={col.header} tooltip={col.headerTooltip} />
            </span>
          );
        }

        return (
          <div
            key={col.id}
            role="columnheader"
            className={cn(
              'flex min-w-0 items-center gap-1 overflow-hidden break-words',
              alignClass,
              isPinned &&
                `${PINNED_FILL_HEADER} -ml-4 pl-4 sticky left-0 ${pinnedEdge('start', overlap.start)}`,
              isPinnedEnd &&
                `${PINNED_FILL_HEADER} -mr-4 justify-center pl-2 pr-2 sticky right-0 ${pinnedEdge('end', overlap.end)}`,
            )}
          >
            {hasSort && (
              <button
                type="button"
                onClick={() => onSort(col.id)}
                // Buttons reset text-transform, font and tracking; inherit the header row's
                // overline treatment so every column title is drawn the same way.
                className="group/sort inline-flex items-center gap-1 rounded [font:inherit] [letter-spacing:inherit] [text-transform:inherit] hover:text-foreground focus:outline-none focus-visible:ring-1 focus-visible:ring-accent"
                aria-label={`Sort by ${col.header}`}
              >
                {col.header}
                <SortIcon dir={sortDir} />
              </button>
            )}
            {hasSort && col.headerTooltip && (
              <HeaderInfo label={col.header} tooltip={col.headerTooltip} />
            )}
            {!hasSort && <HeaderLabel label={col.header} tooltip={col.headerTooltip} />}
            {hasFilter && (
              <FilterButton
                colId={col.id}
                options={
                  col.filter!.mode === 'client'
                    ? (col.filter as DataTableClientFilter<TData>).options
                    : (col.filter as DataTableServerFilter).options
                }
                searchable={col.filter!.searchable}
                mono={col.filter!.mono}
                selected={filterSelected}
                active={filterActive}
                onToggle={(val) => {
                  if (col.filter?.mode === 'client') {
                    onToggleClientFilter(col.id, val);
                  } else if (col.filter?.mode === 'server') {
                    (col.filter as DataTableServerFilter).onToggle(val);
                  }
                }}
                onClear={() => {
                  if (col.filter?.mode === 'client') {
                    onClearClientFilter(col.id);
                  } else if (col.filter?.mode === 'server') {
                    (col.filter as DataTableServerFilter).onClear?.();
                  }
                }}
              />
            )}
          </div>
        );
      })}
    </div>
  );
}

// ─── Internal: Sort icon ───────────────────────────────────────────────────────

function SortIcon({ dir }: { dir: 'asc' | 'desc' | null }) {
  if (dir === 'asc')
    return (
      <span className="shrink-0 inline-flex">
        <Icon name="arrow-up" size="2xs" />
      </span>
    );
  if (dir === 'desc')
    return (
      <span className="shrink-0 inline-flex">
        <Icon name="arrow-down" size="2xs" />
      </span>
    );
  return (
    <span className="shrink-0 inline-flex opacity-40 group-hover/sort:opacity-70 transition-opacity">
      <Icon name="arrow-up-down" size="2xs" />
    </span>
  );
}

// ─── Internal: Filter button ───────────────────────────────────────────────────

interface FilterButtonProps {
  colId: string;
  options: DataTableFilterOption[];
  searchable?: boolean;
  mono?: boolean;
  selected: Set<string>;
  active: boolean;
  onToggle: (value: string) => void;
  onClear: () => void;
}

function FilterButton({
  options,
  searchable,
  mono,
  selected,
  active,
  onToggle,
  onClear,
}: FilterButtonProps) {
  const [query, setQuery] = React.useState('');
  const visibleOptions = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? options.filter((o) => o.label.toLowerCase().includes(q)) : options;
  }, [options, query]);

  return (
    <Popover onOpenChange={(open) => !open && setQuery('')}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="Filter"
          className={cn(
            'inline-flex items-center justify-center rounded p-0.5 transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-accent',
            active
              ? 'text-accent bg-accent/10 hover:bg-accent/20'
              : 'text-subtle hover:text-foreground hover:bg-surface-elevated',
          )}
        >
          <Icon name="filter" size="2xs" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className={searchable ? 'w-80' : 'w-48'}>
        {searchable && (
          <div className="relative border-b border-border-subtle p-1.5">
            <div className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-subtle">
              <Icon name="search" size="2xs" />
            </div>
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search…"
              aria-label="Search filter options"
              className="h-8 w-full rounded-md border border-border-subtle bg-surface pl-8 pr-3 text-sm text-foreground placeholder:text-subtle focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
        )}
        <div
          role="listbox"
          aria-multiselectable="true"
          className={cn('p-1.5 space-y-0.5', searchable && 'max-h-64 overflow-y-auto')}
        >
          {visibleOptions.length === 0 && (
            <div className="px-2 py-1.5 text-sm text-subtle">No matches</div>
          )}
          {visibleOptions.map((opt) => {
            const isSelected = selected.has(opt.value);
            return (
              <button
                key={opt.value}
                type="button"
                role="option"
                aria-selected={isSelected}
                onClick={() => onToggle(opt.value)}
                className={cn(
                  'flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-foreground transition-colors hover:bg-surface-elevated',
                  mono && 'font-mono text-xs',
                )}
              >
                <span
                  className={cn(
                    'flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border',
                    isSelected ? 'border-accent bg-accent text-accent-foreground' : 'border-border',
                  )}
                >
                  {isSelected && <Icon name="check" size="2xs" />}
                </span>
                {opt.dot && <span className={cn('h-2 w-2 shrink-0 rounded-full', opt.dot)} />}
                <span className="min-w-0 break-all">{opt.label}</span>
              </button>
            );
          })}
        </div>
        {active && (
          <div className="border-t border-border-subtle p-1.5">
            <button
              type="button"
              onClick={onClear}
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-subtle transition-colors hover:bg-surface-elevated"
            >
              Clear filter{selected.size > 1 ? ` (${selected.size})` : ''}
            </button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

// ─── Internal: Pagination footer ───────────────────────────────────────────────

interface PaginationProps {
  from: number;
  to: number;
  total: number;
  page: number;
  pageCount: number;
  pageSize: number;
  pageSizeOptions: readonly number[];
  onPageChange: (page: number) => void;
  onPageSizeChange: (size: number) => void;
  noun?: string;
}

function DataTablePagination({
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
}: PaginationProps) {
  return (
    <div className="flex items-center gap-3 border-t border-border-subtle px-4 py-[11px]">
      <span className="text-xs whitespace-nowrap text-subtle">
        {from}–{to} of {total}
      </span>
      <span className="grow" />
      <span className="text-xs whitespace-nowrap text-subtle">Rows per page</span>
      <NativeSelect
        className="h-8 w-auto min-w-0 px-2 py-0 text-xs"
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
    </div>
  );
}
