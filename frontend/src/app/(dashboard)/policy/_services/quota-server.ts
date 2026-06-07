import 'server-only';

import { config } from '@orbit/server/config';

// ── Types ─────────────────────────────────────────────────────────────────────

export interface QuotaGroup {
  id: string;
  tenant_id: string;
  name: string;
  monthly_budget_usd: number;
  created_at: string;
  updated_at: string;
}

export interface PrincipalQuota {
  principal_id: string;
  monthly_budget_usd: number;
  has_limit: boolean;
}

type Ok<T> = { ok: true; data: T };
type Err = { ok: false; status: number; message: string };
type Result<T> = Ok<T> | Err;

// ── Internal helper ───────────────────────────────────────────────────────────

async function quotaFetch<T>(
  path: string,
  accessToken: string,
  init?: RequestInit,
): Promise<Result<T>> {
  let res: Response;
  try {
    res = await fetch(`${config.inferenceGatewayUrl}/v1/quotas${path}`, {
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
    console.error(`[quotaFetch] ${path}: ${isTimeout ? 'timeout' : 'network error'}`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Inference gateway timed out' : 'Failed to reach inference gateway',
    };
  }

  if (res.status === 204) return { ok: true, data: undefined as unknown as T };

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[quotaFetch] ${path}: ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  try {
    const data = (await res.json()) as T;
    return { ok: true, data };
  } catch {
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }
}

// ── Groups ────────────────────────────────────────────────────────────────────

export async function fetchQuotaGroups(
  accessToken: string,
): Promise<Result<{ data: QuotaGroup[] }>> {
  return quotaFetch('/groups', accessToken);
}

export async function createQuotaGroup(
  accessToken: string,
  body: { name: string; monthly_budget_usd: number },
): Promise<Result<{ data: QuotaGroup }>> {
  return quotaFetch('/groups', accessToken, {
    method: 'POST',
    body: JSON.stringify(body),
  });
}

export async function updateQuotaGroupBudget(
  accessToken: string,
  id: string,
  monthly_budget_usd: number,
): Promise<Result<void>> {
  return quotaFetch(`/groups/${encodeURIComponent(id)}`, accessToken, {
    method: 'PUT',
    body: JSON.stringify({ monthly_budget_usd }),
  });
}

export async function deleteQuotaGroup(accessToken: string, id: string): Promise<Result<void>> {
  return quotaFetch(`/groups/${encodeURIComponent(id)}`, accessToken, {
    method: 'DELETE',
  });
}

// ── Principal quotas ──────────────────────────────────────────────────────────

export async function fetchPrincipalQuota(
  accessToken: string,
  principalId: string,
): Promise<Result<{ data: PrincipalQuota }>> {
  return quotaFetch(`/principals/${encodeURIComponent(principalId)}`, accessToken);
}

export async function setPrincipalQuota(
  accessToken: string,
  principalId: string,
  monthly_budget_usd: number,
): Promise<Result<void>> {
  return quotaFetch(`/principals/${encodeURIComponent(principalId)}`, accessToken, {
    method: 'PUT',
    body: JSON.stringify({ monthly_budget_usd }),
  });
}

export async function clearPrincipalQuota(
  accessToken: string,
  principalId: string,
): Promise<Result<void>> {
  return quotaFetch(`/principals/${encodeURIComponent(principalId)}`, accessToken, {
    method: 'DELETE',
  });
}
