'use client';

/**
 * @tier 2 — kora-domain-aware: reads and writes `preferences.tables.columns`
 * via `usePreferences()`. Used by: model-gateway, policy, catalog, analytics.
 *
 * USAGE SCOPE: Requires `<PreferencesProvider>` in the ancestor tree, which is
 * only mounted in `(dashboard)/layout.tsx`. Do not use this component outside
 * the authenticated dashboard shell — `usePreferences()` will throw.
 *
 * Wraps Hades' DataTable and connects column visibility to the user's
 * persisted preferences. `tableId` is a required prop that keys the stored
 * visibility map in `tables.columns.<tableId>`.
 *
 * Every table that uses this wrapper gets preference-backed column visibility
 * with no additional wiring — Hades has no awareness of preferences.
 *
 * ## SmartTable vs DataTable decision tree
 *
 * ```
 * Are you building inside (dashboard)/ (authenticated shell)?
 * ├── No  → use DataTable from @runkora/hades
 * └── Yes → Does this table need column visibility persisted per user?
 *           ├── No  → use DataTable from @runkora/hades
 *           └── Yes → use SmartTable from @runkora/components  ← default choice
 * ```
 */

import { DataTable } from '@runkora/hades';
import type { DataTableProps } from '@runkora/hades';
import { usePreferences } from '../app/(dashboard)/_preferences/preferences-context';

// Re-export column and filter types for caller convenience — one import covers all.
export type { DataTableColumn } from '@runkora/hades';
export type {
  DataTableColumnFilter,
  DataTableClientFilter,
  DataTableServerFilter,
  DataTableFilterOption,
} from '@runkora/hades';

// ── Props ─────────────────────────────────────────────────────────────────────

export interface SmartTableProps<TData> extends DataTableProps<TData> {
  /** Stable identifier for this table — keys the column visibility preference. */
  tableId: string;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function SmartTable<TData>({ tableId, ...rest }: SmartTableProps<TData>) {
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
    <DataTable
      {...rest}
      // initialColumnVisibility is a mount-time seed only. Preference changes
      // made in another tab are not reflected until the page reloads.
      initialColumnVisibility={storedVisibility}
      onColumnVisibilityChange={handleColumnVisibilityChange}
    />
  );
}
