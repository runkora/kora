// ── Policy wire types ────────────────────────────────────────────────────────
//
// Mirrors the JSON shapes returned by the policy-service (PAP) and
// policy-gateway (PDP). Feature-local — NOT in shared/api.ts.
// Only promote to shared when another feature module needs them.

// ── Budget ───────────────────────────────────────────────────────────────────

export interface BudgetAssignment {
  /** Per-window ceiling in USD. Omitted/0 on per-run-only grant budgets. */
  limit_usd?: number;
  /** "weekly" or "monthly". */
  reset_cadence: string;
  /** Roles only: true = one shared pot for all members; false = per-member. */
  pooled?: boolean;
  /** Grants only: windowless per-run ceiling for agent runs. */
  per_run_limit_usd?: number;
}

export interface TenantBudget {
  tenant_id: string;
  limit_usd: number;
  reset_cadence: string;
  updated_at?: string;
}

// ── Grants ───────────────────────────────────────────────────────────────────

export type PrincipalType = 'AGENT' | 'HUMAN' | 'PAT' | 'SERVICE';

export interface Grant {
  id: string;
  tenant_id: string;
  principal_type: PrincipalType;
  principal_id: string;
  scopes: string[];
  budget?: BudgetAssignment | null;
  created_at: string;
}

export interface GrantsListResponse {
  grants: Grant[];
  total: number;
  limit: number;
  offset: number;
}

export interface CreateGrantRequest {
  tenant_id?: string;
  principal_type: PrincipalType;
  principal_id: string;
  scopes: string[];
  budget?: BudgetAssignment | null;
}

// ── Roles ────────────────────────────────────────────────────────────────────

export interface RoleEntry {
  id: string;
  tenant_id: string;
  role: string;
  scopes: string[];
  budget?: BudgetAssignment | null;
  created_at: string;
}

export interface RolesListResponse {
  roles: RoleEntry[];
}

export interface UpsertRoleRequest {
  tenant_id?: string;
  role: string;
  scopes: string[];
  budget?: BudgetAssignment | null;
}

// ── Vocabulary ───────────────────────────────────────────────────────────────

export interface ScopeEntry {
  scope: string;
  resource_type: string;
  path: string[];
  action: string;
  description?: string;
  created_at: string;
}

export interface ScopeEntryWithUsage extends ScopeEntry {
  grant_count: number;
  role_count: number;
}

export interface VocabularyListResponse {
  scopes: ScopeEntry[];
}

export interface VocabularyWithUsageListResponse {
  scopes: ScopeEntryWithUsage[];
}

// ── Routes ───────────────────────────────────────────────────────────────────

export interface RouteScope {
  path_prefix: string;
  method: string;
  required_scope: string;
}

export interface ScopeDeclaration {
  scope: string;
  resource_type: string;
  path: string[];
  action: string;
  description?: string;
}

export interface RouteRegistration {
  service: string;
  tenant_id: string;
  routes: RouteScope[];
  scopes?: ScopeDeclaration[];
  registered_at: string;
}

export interface RoutesListResponse {
  registrations: RouteRegistration[];
}

// ── Stats ────────────────────────────────────────────────────────────────────

export interface StatsResponse {
  grants: {
    total: number;
    by_principal_type: Record<PrincipalType, number>;
  };
  roles: {
    total: number;
    reserved: number;
    custom: number;
  };
  vocabulary: {
    total: number;
    by_resource_type: Record<string, number>;
  };
  routes: {
    total: number;
  };
  bundles: {
    tenants: TenantBundleStatus[];
  };
}

export interface TenantBundleStatus {
  tenant_id: string;
  last_compiled_at: string;
  status: 'current' | 'stale';
}

// ── Scopes Explain (policy-gateway) ──────────────────────────────────────────

export type { GrantDetail, RoleDetail, ScopesExplainResponse } from '@runkora/shared/api';

// ── Check Simulate (policy-gateway) ──────────────────────────────────────────

export interface CheckSimulateRequest {
  principal_type: PrincipalType;
  principal_id: string;
  roles?: string[];
  resource: {
    type: string;
    path?: string[];
    attributes?: Record<string, string>;
  };
  action: string;
}

export interface CheckResponse {
  allow: boolean;
  reason: string;
}
