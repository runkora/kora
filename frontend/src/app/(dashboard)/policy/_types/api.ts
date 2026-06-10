// ── Policy wire types ────────────────────────────────────────────────────────
//
// Mirrors the JSON shapes returned by the policy-service (PAP) and
// policy-gateway (PDP). Feature-local — NOT in shared/api.ts.
// Only promote to shared when another feature module needs them.

// ── Budget ───────────────────────────────────────────────────────────────────

export interface BudgetAssignment {
  monthly_limit_usd: number;
  reset_cadence: string;
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

export interface GrantDetail {
  grant_id: string;
  scopes: string[];
}

export interface RoleDetail {
  role: string;
  scopes: string[];
}

export interface ScopesExplainResponse {
  principal_type: string;
  principal_id: string;
  tenant_id: string;
  from_roles: RoleDetail[];
  from_grants: GrantDetail[];
  effective_scopes: string[];
  evaluated_at: string;
}

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
