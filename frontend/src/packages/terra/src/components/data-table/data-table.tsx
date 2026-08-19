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
  /**
   * CSS grid column sizing value for this column.
   * Examples: '1fr', '168px', 'minmax(0, 1.1fr)', '74px'
   * Values are assembled into the grid-template-columns string in column order.
   * Defaults to '1fr' when omitted.
   */
  gridWidth?: string;
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
          vis[col.id] = initialColumnVisibility[col.id] ?? true;
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
      const first = next[0];
      if (first) onSortChange?.(first.id, first.desc ? 'desc' : 'asc');
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

  const gridTemplateColumns = visibleColDefs.map((c) => c.gridWidth ?? '1fr').join(' ');

  // ── Derived display state ───────────────────────────────────────────────────

  const hasActiveFilters =
    searchQuery.trim().length > 0 ||
    Object.values(clientFilters).some((s) => s.size > 0) ||
    columnDefs.some(
      (c) => c.filter?.mode === 'server' && (c.filter as DataTableServerFilter).selected.size > 0,
    );

  const pinnedColId = columnDefs.find((c) => c.locked)?.id;

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
                    return (
                      <div
                        key={colDef.id}
                        role="cell"
                        className={cn(
                          'min-w-0',
                          colDef.align === 'right' && 'text-right',
                          colDef.align === 'center' && 'text-center',
                          isPinned &&
                            'sticky left-0 z-10 bg-surface group-hover:bg-surface-elevated/60',
                        )}
                      >
                        {colDef.cell(row.original)}
                      </div>
                    );
                  })}
                </div>
              );
            })
          )}
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
}

function DataTableHeader<TData>({
  columnDefs,
  sorting,
  clientFilters,
  onSort,
  onToggleClientFilter,
  onClearClientFilter,
  pinnedColId,
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
              className={cn(alignClass, isPinned && 'sticky left-0 z-10')}
            >
              {col.header}
            </span>
          );
        }

        return (
          <div
            key={col.id}
            role="columnheader"
            className={cn('flex items-center gap-1', alignClass, isPinned && 'sticky left-0 z-10')}
          >
            {hasSort && (
              <button
                type="button"
                onClick={() => onSort(col.id)}
                className="group/sort inline-flex items-center gap-1 hover:text-foreground focus:outline-none focus-visible:ring-1 focus-visible:ring-accent rounded"
                aria-label={`Sort by ${col.header}`}
              >
                {col.header}
                <SortIcon dir={sortDir} />
              </button>
            )}
            {!hasSort && <span>{col.header}</span>}
            {hasFilter && (
              <FilterButton
                colId={col.id}
                options={
                  col.filter!.mode === 'client'
                    ? (col.filter as DataTableClientFilter<TData>).options
                    : (col.filter as DataTableServerFilter).options
                }
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
  selected: Set<string>;
  active: boolean;
  onToggle: (value: string) => void;
  onClear: () => void;
}

function FilterButton({ options, selected, active, onToggle, onClear }: FilterButtonProps) {
  return (
    <Popover>
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
      <PopoverContent align="start" className="w-48">
        <div className="p-1.5 space-y-0.5">
          {options.map((opt) => {
            const isSelected = selected.has(opt.value);
            return (
              <button
                key={opt.value}
                type="button"
                onClick={() => onToggle(opt.value)}
                className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-foreground transition-colors hover:bg-surface-elevated"
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
                {opt.label}
              </button>
            );
          })}
          {active && (
            <button
              type="button"
              onClick={onClear}
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-subtle transition-colors hover:bg-surface-elevated mt-1 pt-1 border-t border-border-subtle"
            >
              Clear filter
            </button>
          )}
        </div>
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
