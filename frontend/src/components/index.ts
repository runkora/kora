export { PrincipalBadge } from './principal-badge';
export type { PrincipalBadgeProps, PrincipalType } from './principal-badge';

/** Maps an OrbitStatus value to a labelled Badge with a status dot. */
export { StatusBadge } from './status-badge';
export type { StatusBadgeProps, OrbitStatus } from './status-badge';

/** KPI display card: icon + label + value + optional delta/breakdown/footer link. */

/** Single row in a divide-y activity feed: status dot + label + meta + message + timestamp. */
export { EventFeedItem } from './event-feed-item';
export type { EventFeedItemProps } from './event-feed-item';

/** Syntax-highlighted kora scope chip (resource:path:action). */
export { ScopeChip } from './scope-chip';
export type { ScopeChipProps } from './scope-chip';
export { ScopeChipList } from './scope-chip-list';
export type { ScopeChipListProps } from './scope-chip-list';
export { useClampedRows, rowIndices } from './use-clamped-rows';
export { ClampedList } from './clamped-list';
export type { ClampedListProps } from './clamped-list';
export { scopeColumnFilter } from './scope-column-filter';

/** Tri-state collapsible-card scope selector grouped by resource type, optionally searchable. */
export { ScopePicker } from './scope-picker';
export type { ScopePickerProps } from './scope-picker';

/** Light/dark theme toggle via next-themes. */
export { ThemeToggle } from './theme-toggle';

/**
 * Preference-backed DataTable. Requires PreferencesProvider (dashboard-scoped).
 * Default table component for the authenticated dashboard shell.
 */
export { SmartTable } from './smart-table';
export type {
  SmartTableProps,
  DataTableColumn,
  DataTableColumnFilter,
  DataTableClientFilter,
  DataTableServerFilter,
  DataTableFilterOption,
} from './smart-table';

/** Preference-backed period selector (7d/30d/90d). Requires PreferencesProvider (dashboard-scoped). */
export { PeriodSelector } from './period-selector';
export type { PeriodSelectorProps, PeriodPrefKey } from './period-selector';

export { EffectiveAccessPanel } from './effective-access-panel';
export { ModelPicker } from './model-picker';

/** Per-connection action grants as collapsible tri-state cards. */
export { ConnectionCards } from './connection-cards';
