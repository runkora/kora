// ── Feature-local types ───────────────────────────────────────────────────────
//
// Single import point for all types used across the policy feature.
// Components import from here, never directly from `_types/api`.

export type {
  PrincipalType,
  BudgetAssignment,
  TenantBudget,
  Grant,
  GrantsListResponse,
  CreateGrantRequest,
  RoleEntry,
  RolesListResponse,
  UpsertRoleRequest,
  ScopeEntry,
  ScopeEntryWithUsage,
  VocabularyWithUsageListResponse,
  RouteScope,
  RouteRegistration,
  RoutesListResponse,
  StatsResponse,
  TenantBundleStatus,
  GrantDetail,
  RoleDetail,
  ScopesExplainResponse,
  CheckSimulateRequest,
  CheckResponse,
} from './api';
