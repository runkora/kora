import 'server-only';

import { config } from '@runkora/server/config';
import type { ScopesExplainResponse } from '@runkora/shared/api';

// ── Result types ─────────────────────────────────────────────────────────────

type Ok<T> = { ok: true; data: T };
type Err = { ok: false; status: number; message: string };
export type Result<T> = Ok<T> | Err;

// ── Internal fetch helper ────────────────────────────────────────────────────

export async function policyGatewayFetch<T>(
  path: string,
  accessToken: string,
  init?: RequestInit,
): Promise<Result<T>> {
  let res: Response;
  try {
    res = await fetch(`${config.policyGatewayUrl}/policy${path}`, {
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
    console.error(`[policyGatewayFetch] ${path}: ${isTimeout ? 'timeout' : 'network error'}`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Policy gateway timed out' : 'Failed to reach policy gateway',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[policyGatewayFetch] ${path}: ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  try {
    const data = (await res.json()) as T;
    return { ok: true, data };
  } catch {
    console.error(`[policyGatewayFetch] ${path}: failed to parse JSON`);
    return { ok: false, status: 502, message: 'Invalid response from policy gateway' };
  }
}

// ── Policy Gateway (PDP) endpoints ───────────────────────────────────────────

export async function fetchScopesExplain(
  accessToken: string,
  principalType?: string,
  principalId?: string,
): Promise<Result<ScopesExplainResponse>> {
  const params = new URLSearchParams();
  if (principalType) params.set('principal_type', principalType);
  if (principalId) params.set('principal_id', principalId);
  const qs = params.toString();
  return policyGatewayFetch(`/scopes/explain${qs ? `?${qs}` : ''}`, accessToken);
}
