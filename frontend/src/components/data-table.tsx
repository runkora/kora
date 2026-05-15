'use client';

/**
 * @tier 2 — Orbit-domain-aware: reads and writes `preferences.tables.columns`
 * via `usePreferences()`. Used by: model-gateway, policy, catalog.
 *
 * USAGE SCOPE: Requires `<PreferencesProvider>` in the ancestor tree, which is
 * only mounted in `(dashboard)/layout.tsx`. Do not use this component outside
 * the authenticated dashboard shell — `usePreferences()` will throw.
 *
 * Wraps Terra's DataTable and connects column visibility to the user's
 * persisted preferences. `tableId` is a required prop that keys the stored
 * visibility map in `tables.columns.<tableId>`.
 *
 * Every table that uses this wrapper gets preference-backed column visibility
 * with no additional wiring — Terra has no awareness of preferences.
 */

import { DataTable as TerraDataTable } from '@orbit/terra';
import type { DataTableProps as TerraDataTableProps } from '@orbit/terra';

import { usePreferences } from '../app/(dashboard)/_preferences/preferences-context';

// Re-export for convenience so callers only need one import.
export type { DataTableColumn } from '@orbit/terra';
export type {
  DataTableColumnFilter,
  DataTableClientFilter,
  DataTableServerFilter,
} from '@orbit/terra';

// ── Props ─────────────────────────────────────────────────────────────────────

export interface DataTableProps<TData> extends TerraDataTableProps<TData> {
  /** Stable identifier for this table — keys the column visibility preference. */
  tableId: string;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function DataTable<TData>({ tableId, ...rest }: DataTableProps<TData>) {
  const { preferences, updatePreferences } = usePreferences();
  const storedVisibility = preferences.tables.columns[tableId] ?? {};

  function handleColumnVisibilityChange(columnId: string, visible: boolean) {
    const defaultVisible = rest.columns.find((c) => c.id === columnId)?.defaultVisible ?? true;
    updatePreferences((prev) => {
      const tableMap = { ...prev.tables.columns[tableId] };
      if (visible === defaultVisible) {
        delete tableMap[columnId];
      } else {
        tableMap[columnId] = visible;
      }
      return {
        ...prev,
        tables: {
          ...prev.tables,
          columns: { ...prev.tables.columns, [tableId]: tableMap },
        },
      };
    });
  }

  return (
    <TerraDataTable
      {...rest}
      // initialColumnVisibility is a mount-time seed only. Preference changes
      // made in another tab are not reflected until the page reloads.
      initialColumnVisibility={storedVisibility}
      onColumnVisibilityChange={handleColumnVisibilityChange}
    />
  );
}
