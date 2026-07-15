// ── Dev Environments API — client-side BFF calls ──────────────────────────────
//
// All functions call the BFF routes under /api/dev-environments. Never
// throws — callers pattern-match on the returned `error` field.

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type {
  DevEnvironment,
  DevEnvironmentCreateRequest,
  DevEnvironmentExtendRequest,
  DevEnvironmentKubeconfigResponse,
  DevEnvironmentLimits,
} from '@orbit/shared/api';

async function errorMessage(res: Response): Promise<string> {
  const body = await res.json().catch(() => ({}) as { message?: string });
  return (body as { message?: string }).message ?? `Request failed (${res.status})`;
}

export async function fetchDevEnvironments(): Promise<{
  data: DevEnvironment[];
  limits: DevEnvironmentLimits | null;
  error: string | null;
}> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.devEnvironments.list(), { cache: 'no-store' });
  } catch {
    return { data: [], limits: null, error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) return { data: [], limits: null, error: await errorMessage(res) };
  const body = (await res.json()) as { data: DevEnvironment[]; limits?: DevEnvironmentLimits };
  return { data: body.data ?? [], limits: body.limits ?? null, error: null };
}

export async function fetchDevEnvironment(
  id: string,
): Promise<{ environment: DevEnvironment | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.devEnvironments.detail(id), { cache: 'no-store' });
  } catch {
    return { environment: null, error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) return { environment: null, error: await errorMessage(res) };
  const body = (await res.json()) as { data: DevEnvironment };
  return { environment: body.data, error: null };
}

export async function createDevEnvironment(
  payload: DevEnvironmentCreateRequest,
): Promise<{ environment: DevEnvironment | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.devEnvironments.create(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { environment: null, error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) return { environment: null, error: await errorMessage(res) };
  const body = (await res.json()) as { data: DevEnvironment };
  return { environment: body.data, error: null };
}

export async function extendDevEnvironment(
  id: string,
  payload: DevEnvironmentExtendRequest,
): Promise<{ environment: DevEnvironment | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.devEnvironments.extend(id), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { environment: null, error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) return { environment: null, error: await errorMessage(res) };
  const body = (await res.json()) as { data: DevEnvironment };
  return { environment: body.data, error: null };
}

export async function wakeDevEnvironment(
  id: string,
): Promise<{ environment: DevEnvironment | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.devEnvironments.wake(id), { method: 'POST' });
  } catch {
    return { environment: null, error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) return { environment: null, error: await errorMessage(res) };
  const body = (await res.json()) as { data: DevEnvironment };
  return { environment: body.data, error: null };
}

export async function kubeconfigDevEnvironment(
  id: string,
): Promise<{ kubeconfig: DevEnvironmentKubeconfigResponse | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.devEnvironments.kubeconfig(id), { method: 'POST' });
  } catch {
    return { kubeconfig: null, error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) return { kubeconfig: null, error: await errorMessage(res) };
  const body = (await res.json()) as { data: DevEnvironmentKubeconfigResponse };
  return { kubeconfig: body.data, error: null };
}

export async function destroyDevEnvironment(id: string): Promise<{ error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.devEnvironments.destroy(id), { method: 'DELETE' });
  } catch {
    return { error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) return { error: await errorMessage(res) };
  return { error: null };
}
