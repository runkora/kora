'use client';

/**
 * Terra – DataTable
 *
 * The opinionated high-level table component. Encapsulates search, sort,
 * client/server-side filtering, and column visibility — all driven from a
 * column-definition array and a dataset. Built on @tanstack/react-table
 * internally; consumers never import TanStack directly.
 *
 * ## When to use DataTable vs raw primitives
 *
 * | Use case                                       | Component              |
 * |------------------------------------------------|------------------------|
 * | Sort / filter / column visibility              | DataTable (this file)  |
 * | Static display-only                            | Raw Table primitives   |
 * | Suspense skeleton mirroring DataTable layout   | Raw Table primitives   |
 *
 * For static, read-only tables with no interactive controls, use the raw
 * Table primitives directly — DataTable adds TanStack overhead that provides
 * no benefit when there is nothing to sort or filter.
 *
 * Client-side filter example:
 * ```tsx
 * <DataTable
 *   data={models}
 *   columns={[
 *     {
 *       id: 'name',
 *       header: 'Model',
 *       cell: (row) => <Text mono>{row.id}</Text>,
 *       sortable: true,
 *       sortAccessor: (row) => row.name,
 *       locked: true,
 *     },
 *     {
 *       id: 'status',
 *       header: 'Status',
 *       cell: (row) => <Badge>{row.status}</Badge>,
 *       filter: {
 *         mode: 'client',
 *         options: [{ value: 'available', label: 'Available', dot: 'bg-status-healthy' }],
 *         filterFn: (row, selected) => selected.has(row.status),
 *       },
 *     },
 *   ]}
 *   getRowKey={(row) => row.id}
 *   noun="models"
 *   searchPlaceholder="Search models…"
 *   searchFn={(row, q) => row.name.toLowerCase().includes(q)}
 *   defaultSortId="name"
 * />
 * ```
 *
 * Server-side filter (filter state triggers a new fetch / navigation):
 * ```tsx
 * {
 *   id: 'key',
 *   header: 'Key',
 *   cell: (row) => <Text>{row.keyName}</Text>,
 *   filter: {
 *     mode: 'server',
 *     options: keyOptions,
 *     selected: new Set(selectedKeyId ? [selectedKeyId] : []),
 *     onToggle: (val) => navigateWithKey(val === selectedKeyId ? '' : val),
 *     onClear: () => navigateWithKey(''),
 *   },
 * }
 * ```
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
import {
  TableRoot,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  TableEmpty,
} from './table';
import type { TableColumnVisibility } from './table';
import { TableContainer } from './table-extras';
import { TableToolbar } from './table-toolbar';
import { TableColumnHeader } from './table-column-header';
import type { TableColumnFilter, TableColumnFilterOption } from './table-column-header';

// ─── Column filter types ──────────────────────────────────────────────────────

/**
 * Client-side column filter.
 *
 * Terra owns the `selected` Set. Rows are pre-filtered in-component before
 * being handed to TanStack Table. No external state management required.
 */
export interface DataTableClientFilter<TData> {
  mode: 'client';
  /** Options rendered in the filter popover. */
  options: TableColumnFilterOption[];
  /**
   * Return true if the row should be included given the active selections.
   * Only called when `selected.size > 0`.
   */
  filterFn: (row: TData, selected: Set<string>) => boolean;
}

/**
 * Server-side column filter.
 *
 * The consumer owns the selected state and is responsible for triggering a
 * new data fetch when selection changes. Terra renders the filter UI and
 * delegates all state changes to the provided callbacks.
 *
 * Typical use: URL-driven navigation, API query params.
 */
export interface DataTableServerFilter {
  mode: 'server';
  /** Options rendered in the filter popover. */
  options: TableColumnFilterOption[];
  /** Currently selected values — managed externally by the consumer. */
  selected: Set<string>;
  /** Called when the user toggles an option. */
  onToggle: (value: string) => void;
  /** Called when the user clears the filter. Omit to hide the clear button. */
  onClear?: () => void;
}

/** Union of client-managed and server-managed filter modes. */
export type DataTableColumnFilter<TData> = DataTableClientFilter<TData> | DataTableServerFilter;

// ─── Column definition ────────────────────────────────────────────────────────

/** Definition for a single column in a DataTable. */
export interface DataTableColumn<TData> {
  /** Unique stable identifier. Used as a React key and in visibility state. */
  id: string;
  /** Column header label, shown in the `<th>` and the columns-toggle popover. */
  header: string;
  /** Renders the cell content for a given data row. */
  cell: (row: TData) => React.ReactNode;
  /** Whether a sort button is rendered on this column. Defaults to false. */
  sortable?: boolean;
  /**
   * Extract the primitive value used for sorting comparisons.
   * Required when `sortable` is true. Return a string or number.
   */
  sortAccessor?: (row: TData) => string | number;
  /** Initial column visibility. Defaults to true. */
  defaultVisible?: boolean;
  /**
   * Locked columns are always visible and excluded from the columns-toggle
   * popover. Use for primary identifier or always-required columns.
   */
  locked?: boolean;
  /** Header and cell alignment. Defaults to 'left'. */
  align?: 'left' | 'right' | 'center';
  /** Optional fixed column width (CSS value string or px number). */
  width?: string | number;
  /** Column-level filter. Omit for non-filterable columns. */
  filter?: DataTableColumnFilter<TData>;
}

// ─── Table props ──────────────────────────────────────────────────────────────

export interface DataTableProps<TData> {
  /** The full (client-side) or current-page (server-side) dataset. */
  data: TData[];
  /** Column definitions describing headers, cells, sort, filter, and visibility. */
  columns: DataTableColumn<TData>[];
  /** Extract a stable React key from a row. */
  getRowKey: (row: TData) => string;
  /**
   * Override the total record count shown in the toolbar.
   * Use for server-paginated tables where `data` is a single page.
   * Defaults to `data.length`.
   */
  totalCount?: number;
  /** Row noun for the count label. E.g. "models" → "Showing 3 of 10 models". */
  noun?: string;
  /**
   * Placeholder text for the search input.
   * Must be paired with `searchFn`; omit both to hide the search box.
   */
  searchPlaceholder?: string;
  /**
   * Full-text search predicate. Return true if the row matches the query.
   * Must be provided to enable the search input.
   */
  searchFn?: (row: TData, query: string) => boolean;
  /** Column id to sort by on initial render. */
  defaultSortId?: string;
  /** Initial sort direction. Defaults to 'asc'. */
  defaultSortDir?: 'asc' | 'desc';
  /**
   * Error content rendered in place of the table rows.
   * When provided, the empty state and row list are both suppressed.
   */
  error?: React.ReactNode;
  /** Message shown when no data is present and no filters are active. */
  emptyMessage?: string;
  /** Message shown when active search or filters produce zero results. */
  filteredEmptyMessage?: string;
  /** Called when a data row is clicked. Adds a pointer cursor to rows. */
  onRowClick?: (row: TData) => void;
  /**
   * Description announced by screen readers when a clickable row receives focus.
   * Only used when `onRowClick` is provided. Defaults to `"View details"`.
   */
  onRowClickLabel?: string;
  /** Extra content rendered in the toolbar after the search input. */
  toolbarActions?: React.ReactNode;
}

// ─── Component ────────────────────────────────────────────────────────────────

/**
 * DataTable
 *
 * A fully data-driven table component. Pass your dataset and column definitions;
 * DataTable handles search, client-side filtering, sort cycling, column
 * visibility, and toolbar layout internally. Built on TanStack Table — consumers
 * never import TanStack directly.
 */
export function DataTable<TData>({
  data,
  columns: columnDefs,
  getRowKey,
  totalCount,
  noun = 'records',
  searchPlaceholder,
  searchFn,
  defaultSortId,
  defaultSortDir = 'asc',
  error,
  emptyMessage = 'No records found.',
  filteredEmptyMessage = 'No records match your filters.',
  onRowClick,
  onRowClickLabel = 'View details',
  toolbarActions,
}: DataTableProps<TData>) {
  // ── Internal state ──────────────────────────────────────────────────────────

  const rowDescId = React.useId();

  const [searchQuery, setSearchQuery] = React.useState('');

  // Per-column filter selections for client-mode columns only.
  const [clientFilters, setClientFilters] = React.useState<Record<string, Set<string>>>({});

  const [sorting, setSorting] = React.useState<SortingState>(() =>
    defaultSortId ? [{ id: defaultSortId, desc: defaultSortDir === 'desc' }] : [],
  );

  const [columnVisibility, setColumnVisibility] = React.useState<VisibilityState>(() => {
    const vis: VisibilityState = {};
    columnDefs.forEach((col) => {
      // Locked columns are always visible — omit from visibility state.
      if (!col.locked) {
        vis[col.id] = col.defaultVisible ?? true;
      }
    });
    return vis;
  });

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
        // When sortable, the accessorFn provides the primitive value TanStack
        // sorts on. Non-sortable columns use a no-op accessor.
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

  // ── TanStack table instance ─────────────────────────────────────────────────
  // TanStack Table returns new function references each render by design; this is
  // a known React Compiler incompatibility accepted for `DataTable` only.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: filteredData,
    columns: tanstackColumns,
    state: { sorting, columnVisibility },
    onSortingChange: setSorting,
    onColumnVisibilityChange: setColumnVisibility,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    // Filtering is performed above in the pre-filter useMemo.
    manualFiltering: true,
    enableSortingRemoval: false,
  });

  // ── Fast column-def lookup ──────────────────────────────────────────────────

  const colById = React.useMemo(() => {
    const m = new Map<string, DataTableColumn<TData>>();
    columnDefs.forEach((c) => m.set(c.id, c));
    return m;
  }, [columnDefs]);

  // ── Sort cycling: asc ↔ desc ───────────────────────────────────────────────
  // Two-state toggle: asc on first click, desc on second, back to asc on third.
  // No "null/unsorted" state: for most data sets the natural API order is the
  // same as ascending, so the null state is visually indistinguishable from asc
  // and reads as "the sort button is broken."
  // Uses a functional setSorting update to avoid stale-closure issues.
  const handleSort = React.useCallback(
    (columnId: string) => {
      setSorting((prev) => {
        const existing = prev.find((s) => s.id === columnId);
        if (!existing) return [{ id: columnId, desc: false }]; // unsorted → asc
        return [{ id: columnId, desc: !existing.desc }]; // asc ↔ desc
      });
    },
    [setSorting],
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

  // ── Toolbar data ────────────────────────────────────────────────────────────

  const toolbarColumnItems: TableColumnVisibility[] = columnDefs
    .filter((c) => !c.locked)
    .map((c) => ({
      id: c.id,
      label: c.header,
      visible: table.getColumn(c.id)?.getIsVisible() ?? true,
    }));

  const toolbarSearch = searchFn
    ? {
        value: searchQuery,
        onChange: setSearchQuery,
        placeholder: searchPlaceholder ?? 'Search…',
      }
    : undefined;

  const toolbarCount = {
    showing: table.getRowModel().rows.length,
    total: totalCount ?? data.length,
    label: noun,
  };

  // ── Derived ─────────────────────────────────────────────────────────────────

  const hasActiveFilters =
    searchQuery.trim().length > 0 ||
    Object.values(clientFilters).some((s) => s.size > 0) ||
    columnDefs.some(
      (c) => c.filter?.mode === 'server' && (c.filter as DataTableServerFilter).selected.size > 0,
    );

  const rows = table.getRowModel().rows;
  const visibleColCount = table.getVisibleLeafColumns().length;

  // ── Render ──────────────────────────────────────────────────────────────────

  return (
    <TableContainer>
      {onRowClick && (
        <span id={rowDescId} className="sr-only">
          {onRowClickLabel}
        </span>
      )}
      <TableToolbar
        search={toolbarSearch}
        count={toolbarCount}
        columns={
          toolbarColumnItems.length > 0
            ? {
                items: toolbarColumnItems,
                onToggle: (id) => table.getColumn(id)?.toggleVisibility(),
              }
            : undefined
        }
        actions={toolbarActions}
      />

      <TableRoot>
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  const colDef = colById.get(header.id);
                  if (!colDef) return null;

                  const isSortable = colDef.sortable === true;
                  const hasFilter = colDef.filter !== undefined;

                  // Static header — no sort or filter affordances.
                  if (!isSortable && !hasFilter) {
                    return (
                      <TableHead
                        key={header.id}
                        style={colDef.width !== undefined ? { width: colDef.width } : undefined}
                      >
                        {colDef.header}
                      </TableHead>
                    );
                  }

                  // Build the structured filter prop for TableColumnHeader.
                  let filterProp: TableColumnFilter | undefined;
                  if (colDef.filter) {
                    if (colDef.filter.mode === 'client') {
                      const f = colDef.filter as DataTableClientFilter<TData>;
                      filterProp = {
                        options: f.options,
                        selected: clientFilters[colDef.id] ?? new Set<string>(),
                        onToggle: (v) => toggleClientFilter(colDef.id, v),
                        onClear: () => clearClientFilter(colDef.id),
                      };
                    } else {
                      const f = colDef.filter as DataTableServerFilter;
                      filterProp = {
                        options: f.options,
                        selected: f.selected,
                        onToggle: f.onToggle,
                        onClear: f.onClear,
                      };
                    }
                  }

                  // Map TanStack's false | 'asc' | 'desc' to SortDir (null | 'asc' | 'desc').
                  const tanstackSort = header.column.getIsSorted();
                  const sortDir = tanstackSort === false ? null : tanstackSort;

                  return (
                    <TableColumnHeader
                      key={header.id}
                      align={colDef.align}
                      width={colDef.width}
                      sortDir={isSortable ? sortDir : undefined}
                      onSort={isSortable ? () => handleSort(header.id) : undefined}
                      filter={filterProp}
                    >
                      {colDef.header}
                    </TableColumnHeader>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>

          <TableBody>
            {error ? (
              <TableRow>
                <TableCell colSpan={visibleColCount}>{error}</TableCell>
              </TableRow>
            ) : rows.length === 0 ? (
              <TableEmpty
                colSpan={visibleColCount}
                message={hasActiveFilters ? filteredEmptyMessage : emptyMessage}
              />
            ) : (
              rows.map((row) => (
                <TableRow
                  key={getRowKey(row.original)}
                  style={onRowClick ? { cursor: 'pointer' } : undefined}
                  onClick={onRowClick ? () => onRowClick(row.original) : undefined}
                  tabIndex={onRowClick ? 0 : undefined}
                  aria-describedby={onRowClick ? rowDescId : undefined}
                  onKeyDown={
                    onRowClick
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
                >
                  {row.getVisibleCells().map((cell) => {
                    const cellColDef = colById.get(cell.column.id);
                    return (
                      <TableCell key={cell.id}>
                        {cellColDef ? cellColDef.cell(row.original) : null}
                      </TableCell>
                    );
                  })}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </TableRoot>
    </TableContainer>
  );
}
