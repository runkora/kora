'use client';

/**
 * Terra – TableToolbar
 *
 * A single, opaque component for the strip above a data table.
 * Pass the data it needs — layout, ordering, and spacing are owned internally.
 *
 * All prop groups are optional. Omit a group to hide that section:
 *   - No `search`   → no search input
 *   - No `count`    → no record count
 *   - No `columns`  → no columns-toggle button
 *
 * ```tsx
 * <TableToolbar
 *   search={{ value: q, onChange: setQ, placeholder: "Search models…" }}
 *   count={{ showing: filteredRows.length, total: models.length, label: "models" }}
 *   columns={{ items: columnVisibility, onToggle: toggleColumn }}
 * />
 * ```
 */

import * as React from 'react';
import { Check, Search, Settings2 } from 'lucide-react';
import { cn } from '../../utils';
import { Popover, PopoverContent, PopoverTrigger } from '../popover';
import type { TableColumnVisibility } from './table';

// ─── Public interface ─────────────────────────────────────────────────────────

export interface TableToolbarProps {
  /** Search box. Providing this prop enables the search input. */
  search?: {
    value: string;
    onChange: (value: string) => void;
    /** Defaults to "Search…" */
    placeholder?: string;
  };

  /** Record count. Providing this prop enables the "Showing X of Y" label. */
  count?: {
    showing: number;
    total: number;
    /** Row noun, e.g. "models" → "Showing 3 of 10 models". Defaults to "records". */
    label?: string;
  };

  /** Column visibility. Providing this prop enables the columns-toggle button. */
  columns?: {
    items: TableColumnVisibility[];
    onToggle: (id: string) => void;
  };
}

// ─── Component ────────────────────────────────────────────────────────────────

export function TableToolbar({ search, count, columns }: TableToolbarProps) {
  const hasRight = count !== undefined || columns !== undefined;

  return (
    <div className="flex items-center gap-3 px-4 py-3 border-b border-border-subtle">
      {search && (
        <ToolbarSearch
          value={search.value}
          onChange={search.onChange}
          placeholder={search.placeholder ?? 'Search…'}
        />
      )}

      {hasRight && (
        <div className="ml-auto flex items-center gap-3">
          {count && (
            <ToolbarCount
              showing={count.showing}
              total={count.total}
              label={count.label ?? 'records'}
            />
          )}
          {columns && <ToolbarColumnsToggle columns={columns.items} onToggle={columns.onToggle} />}
        </div>
      )}
    </div>
  );
}

// ─── Private sub-components ───────────────────────────────────────────────────

function ToolbarSearch({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
}) {
  return (
    <div className="relative">
      <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-subtle pointer-events-none" />
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="h-8 w-56 rounded-md border border-border bg-surface pl-8 pr-3 text-sm text-foreground placeholder:text-subtle focus:outline-none focus:ring-1 focus:ring-accent"
      />
    </div>
  );
}

function ToolbarCount({
  showing,
  total,
  label,
}: {
  showing: number;
  total: number;
  label: string;
}) {
  return (
    <span className="text-sm text-subtle whitespace-nowrap">
      Showing {showing.toLocaleString()} of {total.toLocaleString()} {label}
    </span>
  );
}

function ToolbarColumnsToggle({
  columns,
  onToggle,
}: {
  columns: TableColumnVisibility[];
  onToggle: (id: string) => void;
}) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="inline-flex items-center gap-1.5 rounded-md border border-border bg-surface px-3 py-1.5 text-sm font-medium text-foreground shadow-sm transition-colors hover:bg-surface-elevated focus:outline-none focus:ring-1 focus:ring-accent"
        >
          <Settings2 className="h-4 w-4" />
          Columns
        </button>
      </PopoverTrigger>
      <PopoverContent align="end">
        <menu className="min-w-40 p-1.5 space-y-0.5">
          {columns.map((col) => (
            <li key={col.id} className="list-none">
              <button
                type="button"
                onClick={() => onToggle(col.id)}
                className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-foreground transition-colors hover:bg-surface-elevated"
              >
                <span
                  className={cn(
                    'flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border',
                    col.visible ? 'border-accent bg-accent text-white' : 'border-border',
                  )}
                >
                  {col.visible && <Check className="h-3 w-3" />}
                </span>
                {col.label}
              </button>
            </li>
          ))}
        </menu>
      </PopoverContent>
    </Popover>
  );
}
