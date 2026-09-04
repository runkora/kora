import 'server-only';

import { config } from '@orbit/server/config';
import { policyGatewayFetch } from '@orbit/server/services/policy-gateway-server';
import type {
  StatsResponse,
  GrantsListResponse,
  Grant,
  BudgetAssignment,
  TenantBudget,
  CreateGrantRequest,
  RolesListResponse,
  RoleEntry,
  UpsertRoleRequest,
  VocabularyWithUsageListResponse,
  RoutesListResponse,
  CheckSimulateRequest,
  CheckResponse,
} from '../_types/api';

// ── Shared (promoted to src/server/services/) ─────────────────────────────────

export { fetchScopesExplain } from '@orbit/server/services/policy-gateway-server';

// ── Result types ─────────────────────────────────────────────────────────────

type Ok<T> = { ok: true; data: T };
type Err = { ok: false; status: number; message: string };
type Result<T> = Ok<T> | Err;

// ── Internal fetch helper ────────────────────────────────────────────────────

async function policyServiceFetch<T>(
  path: string,
  accessToken: string,
  init?: RequestInit,
): Promise<Result<T>> {
  let res: Response;
  try {
    res = await fetch(`${config.policyServiceUrl}/policy${path}`, {
      ...init,
      headers: {
        Authorization: `Bearer ${accessToken}`,
        'Content-Type': 'application/json',
        ...init?.headers,
      },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    console.error('[policyServiceFetch] request failed', {
      path,
      reason: isTimeout ? 'timeout' : 'network error',
      err,
    });
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Policy service timed out' : 'Failed to reach policy service',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error('[policyServiceFetch] non-OK response', {
      path,
      status: res.status,
      statusText: res.statusText,
    });
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  // 204 No Content — successful mutation with no body.
  if (res.status === 204) {
    return { ok: true, data: undefined } as Result<T>;
  }

  try {
    const data = (await res.json()) as T;
    return { ok: true, data };
  } catch {
    console.error('[policyServiceFetch] failed to parse JSON', { path });
    return { ok: false, status: 502, message: 'Invalid response from policy service' };
  }
}

// ── Policy Service (PAP) endpoints ───────────────────────────────────────────

export async function fetchEffectiveScopes(
  accessToken: string,
): Promise<Result<{ scopes: string[] }>> {
  return policyServiceFetch('/effective-scopes', accessToken);
}

export async function fetchStats(
  accessToken: string,
  tenantId: string,
): Promise<Result<StatsResponse>> {
  return policyServiceFetch(`/stats?tenant_id=${encodeURIComponent(tenantId)}`, accessToken);
}

export async function fetchGrants(
  accessToken: string,
  tenantId: string,
  page = 1,
  limit = 50,
): Promise<Result<GrantsListResponse>> {
  const offset = (page - 1) * limit;
  return policyServiceFetch(
    `/grants?tenant_id=${encodeURIComponent(tenantId)}&limit=${limit}&offset=${offset}`,
    accessToken,
  );
}

export async function createGrant(
  accessToken: string,
  body: CreateGrantRequest,
): Promise<Result<Grant>> {
  return policyServiceFetch('/grants', accessToken, {
    method: 'POST',
    body: JSON.stringify(body),
  });
}

export async function deleteGrant(accessToken: string, id: string): Promise<Result<void>> {
  return policyServiceFetch(`/grants/${encodeURIComponent(id)}`, accessToken, {
    method: 'DELETE',
  });
}

export async function updateGrantBudget(
  accessToken: string,
  id: string,
  budget: BudgetAssignment | null,
): Promise<Result<Grant>> {
  return policyServiceFetch(`/grants/${encodeURIComponent(id)}`, accessToken, {
    method: 'PATCH',
    body: JSON.stringify({ budget }),
  });
}

export async function fetchTenantBudget(
  accessToken: string,
  tenantId: string,
): Promise<Result<TenantBudget>> {
  return policyServiceFetch(
    `/tenant-budget?tenant_id=${encodeURIComponent(tenantId)}`,
    accessToken,
  );
}

export async function upsertTenantBudget(
  accessToken: string,
  tenantId: string,
  limitUsd: number,
  resetCadence: string,
): Promise<Result<TenantBudget>> {
  return policyServiceFetch('/tenant-budget', accessToken, {
    method: 'PUT',
    body: JSON.stringify({ tenant_id: tenantId, limit_usd: limitUsd, reset_cadence: resetCadence }),
  });
}

export async function deleteTenantBudget(
  accessToken: string,
  tenantId: string,
): Promise<Result<void>> {
  return policyServiceFetch(
    `/tenant-budget?tenant_id=${encodeURIComponent(tenantId)}`,
    accessToken,
    {
      method: 'DELETE',
    },
  );
}

export async function fetchRoles(
  accessToken: string,
  tenantId: string,
): Promise<Result<RolesListResponse>> {
  return policyServiceFetch(
    `/roles?tenant_id=${encodeURIComponent(tenantId)}&limit=500`,
    accessToken,
  );
}

export async function upsertRole(
  accessToken: string,
  body: UpsertRoleRequest,
): Promise<Result<RoleEntry>> {
  return policyServiceFetch('/roles', accessToken, {
    method: 'PUT',
    body: JSON.stringify(body),
  });
}

export async function deleteRole(
  accessToken: string,
  tenantId: string,
  role: string,
): Promise<Result<void>> {
  return policyServiceFetch(
    `/roles/${encodeURIComponent(role)}?tenant_id=${encodeURIComponent(tenantId)}`,
    accessToken,
    { method: 'DELETE' },
  );
}

export async function fetchVocabulary(
  accessToken: string,
  tenantId: string,
): Promise<Result<VocabularyWithUsageListResponse>> {
  return policyServiceFetch(`/vocabulary?tenant_id=${encodeURIComponent(tenantId)}`, accessToken);
}

export async function fetchRoutes(accessToken: string): Promise<Result<RoutesListResponse>> {
  return policyServiceFetch('/routes', accessToken);
}

// ── Policy Gateway (PDP) endpoints ───────────────────────────────────────────

export async function checkSimulate(
  accessToken: string,
  body: CheckSimulateRequest,
): Promise<Result<CheckResponse>> {
  return policyGatewayFetch('/check/simulate', accessToken, {
    method: 'POST',
    body: JSON.stringify(body),
  });
}
