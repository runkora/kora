/** Zero-state placeholder: icon + title + description + optional action. */
export { EmptyState } from './empty-state';
export type { EmptyStateProps } from './empty-state';

/** Maps an OrbitStatus value to a labelled Badge with a status dot. */
export { StatusBadge } from './status-badge';
export type { StatusBadgeProps, OrbitStatus } from './status-badge';

/** Destructive-action confirmation modal composing Terra Modal. */
export { ConfirmDialog } from './confirm-dialog';
export type { ConfirmDialogProps } from './confirm-dialog';

/** KPI display card: icon + label + value + optional delta/breakdown/footer link. */
export { MetricCard } from './metric-card';
export type { MetricCardProps } from './metric-card';

/** Single row in a divide-y activity feed: status dot + label + meta + message + timestamp. */
export { EventFeedItem } from './event-feed-item';
export type { EventFeedItemProps } from './event-feed-item';

/** Syntax-highlighted Orbit scope chip (resource:path:action). */
export { ScopeChip } from './scope-chip';
export type { ScopeChipProps } from './scope-chip';

/** Searchable accordion scope selector grouped by resource type. */
export { ScopePicker } from './scope-picker';
export type { ScopePickerProps } from './scope-picker';

/** Tri-state collapsible-card scope selector grouped by resource type. */
export { ScopeTreePicker } from './scope-tree-picker';
export type { ScopeTreePickerProps } from './scope-tree-picker';

/** Backend connection status card, polls BFF /api/identity/health. */
export { HealthStatus } from './health-status';

/** BFF-backed logout button, revokes tokens then redirects to /login. */
export { LogoutButton } from './logout-button';

/** Light/dark theme toggle via next-themes. */
export { ThemeToggle } from './theme-toggle';

/** Preference-backed DataTable. Requires PreferencesProvider (dashboard-scoped). */
export { DataTable } from './data-table';
export type {
  DataTableProps,
  DataTableColumn,
  DataTableColumnFilter,
  DataTableClientFilter,
  DataTableServerFilter,
} from './data-table';

/** Preference-backed period selector (7d/30d/90d). Requires PreferencesProvider (dashboard-scoped). */
export { PeriodSelector } from './period-selector';
export type { PeriodSelectorProps, PeriodPrefKey } from './period-selector';

/** Click-to-copy button with visual feedback (copy → check icon transition). */
export { CopyButton } from './copy-button';
export type { CopyButtonProps } from './copy-button';

/** Input-field-styled copy trigger: shows value text with trailing copy icon, whole field is clickable. */
export { CopyField } from './copy-field';
export type { CopyFieldProps } from './copy-field';

/** Full-width ghost button row with label/value stack and copy icon — for modal detail views. */
export { CopyRow } from './copy-row';
export type { CopyRowProps } from './copy-row';

export { EffectiveAccessPanel } from './effective-access-panel';
export { ModelPicker } from './model-picker';

/** Per-connection action grants as collapsible tri-state cards. */
export { ConnectionCards } from './connection-cards';
