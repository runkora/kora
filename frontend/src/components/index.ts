export { PrincipalBadge, principalLabel } from './principal-badge';
export type { PrincipalBadgeProps, PrincipalType } from './principal-badge';

/** Maps an KoraStatus value to a labelled Badge with a status dot. */
export { StatusBadge } from './status-badge';
export type { StatusBadgeProps, KoraStatus } from './status-badge';

/** Syntax-highlighted kora scope chip (resource:path:action). */
export { ScopeChip } from './scope-chip';
export type { ScopeChipProps } from './scope-chip';
export { ScopeChipList } from './scope-chip-list';
export type { ScopeChipListProps } from './scope-chip-list';
export { useClampedRows, rowIndices } from './use-clamped-rows';
export { ClampedList } from './clamped-list';
export type { ClampedListProps } from './clamped-list';
export { scopeColumnFilter } from './scope-column-filter';

/** Tri-state scope selector grouped by resource type and server or connection, searchable by default. */
export { ScopePicker } from './scope-picker';
export type { ScopePickerProps } from './scope-picker';
export { toScopeOption, scopeOptionsFor } from './scope-option';
export type { ScopeOption } from './scope-option';

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

export { EffectiveAccessPanel } from './effective-access-panel';
export { ModelPicker } from './model-picker';

/** Per-connection action grants as collapsible tri-state cards. */
export { ConnectionCards } from './connection-cards';

/** Each run state's status dot and label. */
export { RUN_STATE_DISPLAY } from './run-state';
export type { RunStateDisplay } from './run-state';

/** A disabled control's tooltip naming the missing permission. */
export { PermissionHint, permissionHintText } from './permission-hint';
export type { PermissionHintProps } from './permission-hint';

/** Card / list layout switch for collection pages. */
export { ViewToggle } from './view-toggle';
export type { ViewToggleProps, ViewMode } from './view-toggle';

/** Pill filter button with a trailing count, for a row of filters above a card view. */
export { FilterChip } from './filter-chip';
export type { FilterChipProps } from './filter-chip';

/** The limits of a budget as switchable rows, laid out like the agent builder's Guardrails. */
export {
  BudgetLimitFields,
  EMPTY_BUDGET_LIMIT_VALUES,
  budgetFromLimitValues,
  budgetLimitProblem,
  budgetLimitValuesFrom,
  budgetLimitsAnyOn,
} from './budget-limit-fields';
export type {
  BudgetLimitFieldsProps,
  BudgetLimitMode,
  BudgetLimitValues,
  BudgetLimits,
} from './budget-limit-fields';

/** Marks items declared in the configuration repository, and pages that are read-only because of it. */
export { ManagedInGitBadge, ManagedInGitCallout, ManagedInGitNotice } from './managed-in-git';
export type { ManagedInGitCalloutProps, ManagedInGitNoticeProps } from './managed-in-git';
