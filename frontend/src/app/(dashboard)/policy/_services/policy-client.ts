// ── Policy client API service ─────────────────────────────────────────────────
//
// Client-side fetch helpers. Call BFF route handlers at /api/policy/* — never
// the Go backend directly. Used by client components for mutations and refresh.

import { ApiRoute } from '@runkora/lib/routing/api-routes';
import type {
  StatsResponse,
  GrantsListResponse,
  Grant,
  CreateGrantRequest,
  RolesListResponse,
  RoleEntry,
  UpsertRoleRequest,
  VocabularyWithUsageListResponse,
  RoutesListResponse,
  ScopesExplainResponse,
  CheckSimulateRequest,
  CheckResponse,
} from '../_types';

// ── Result types ─────────────────────────────────────────────────────────────

interface FetchResult<T> {
  data: T | null;
  error: string | null;
}

async function bffGet<T>(url: string): Promise<FetchResult<T>> {
  let res: Response;
  try {
    res = await fetch(url, { cache: 'no-store' });
  } catch {
    return { data: null, error: 'Network error: could not reach the server.' };
  }

  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const body = (await res.json()) as { message?: string };
      if (body.message) message = body.message;
    } catch {
      /* keep status message */
    }
    return { data: null, error: message };
  }

  const json = (await res.json()) as { data: T };
  return { data: json.data, error: null };
}

async function bffMutate<T>(url: string, method: string, body?: unknown): Promise<FetchResult<T>> {
  let res: Response;
  try {
    res = await fetch(url, {
      method,
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch {
    return { data: null, error: 'Network error: could not reach the server.' };
  }

  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const json = (await res.json()) as { message?: string };
      if (json.message) message = json.message;
    } catch {
      /* keep status message */
    }
    return { data: null, error: message };
  }

  if (res.status === 204) {
    return { data: null, error: null };
  }

  const json = (await res.json()) as { data: T };
  return { data: json.data, error: null };
}

// ── Exports ──────────────────────────────────────────────────────────────────

export async function fetchPolicyStats(): Promise<FetchResult<StatsResponse>> {
  return bffGet(ApiRoute.policyStats.GET());
}

export async function fetchPolicyGrants(): Promise<FetchResult<GrantsListResponse>> {
  return bffGet(ApiRoute.policyGrants.GET());
}

export async function createPolicyGrant(body: CreateGrantRequest): Promise<FetchResult<Grant>> {
  return bffMutate(ApiRoute.policyGrants.POST(), 'POST', body);
}

export async function deletePolicyGrant(id: string): Promise<FetchResult<void>> {
  return bffMutate(ApiRoute.policyGrant.DELETE(id), 'DELETE');
}

export async function fetchPolicyRoles(): Promise<FetchResult<RolesListResponse>> {
  return bffGet(ApiRoute.policyRoles.GET());
}

export async function upsertPolicyRole(body: UpsertRoleRequest): Promise<FetchResult<RoleEntry>> {
  return bffMutate(ApiRoute.policyRoles.PUT(), 'PUT', body);
}

export async function deletePolicyRole(role: string): Promise<FetchResult<void>> {
  return bffMutate(ApiRoute.policyRole.DELETE(role), 'DELETE');
}

export async function fetchPolicyVocabulary(): Promise<
  FetchResult<VocabularyWithUsageListResponse>
> {
  return bffGet(ApiRoute.policyVocabulary.GET());
}

export async function fetchPolicyRoutes(): Promise<FetchResult<RoutesListResponse>> {
  return bffGet(ApiRoute.policyRoutes.GET());
}

export async function fetchPolicyScopesExplain(
  principalType?: string,
  principalId?: string,
): Promise<FetchResult<ScopesExplainResponse>> {
  let url = ApiRoute.policyExplain.GET();
  const params = new URLSearchParams();
  if (principalType) params.set('principal_type', principalType);
  if (principalId) params.set('principal_id', principalId);
  const qs = params.toString();
  if (qs) url = `${url}?${qs}` as typeof url;
  return bffGet(url);
}

export async function simulatePolicyCheck(
  body: CheckSimulateRequest,
): Promise<FetchResult<CheckResponse>> {
  return bffMutate(ApiRoute.policySimulate.POST(), 'POST', body);
}
