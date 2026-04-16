import 'server-only';

import { config } from '@orbit/server/config';

// ── Result types ─────────────────────────────────────────────────────────────

type Ok<T> = { ok: true; data: T };
type Err = { ok: false; status: number; message: string };
type Result<T> = Ok<T> | Err;

// ── Wire types ────────────────────────────────────────────────────────────────

export interface VirtualModelRoute {
  id: string;
  provider: string;
  model_id: string;
  priority: number;
  weight: number;
  input_cost_per_1m: number;
  output_cost_per_1m: number;
  enabled: boolean;
}

export interface VirtualModel {
  id: string;
  family: string;
  name: string;
  display_name: string;
  description: string;
  strategy: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
  routes: VirtualModelRoute[];
}

export interface ProviderModel {
  provider: string;
  model_id: string;
  display_name: string;
  family: string;
  capabilities: string[];
  aliases: string[];
  input_modalities: string[];
  output_modalities: string[];
  dialects: string[];
  enabled: boolean;
}

// ── Internal fetch helper ────────────────────────────────────────────────────

async function catalogFetch<T>(
  path: string,
  accessToken: string,
  init?: RequestInit,
): Promise<Result<T>> {
  let res: Response;
  try {
    res = await fetch(`${config.inferenceGatewayUrl}/catalog${path}`, {
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
    console.error(`[catalogFetch] ${path}: ${isTimeout ? 'timeout' : 'network error'}`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Inference gateway timed out' : 'Failed to reach inference gateway',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[catalogFetch] ${path}: ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  if (res.status === 204) {
    return { ok: true, data: undefined } as Result<T>;
  }

  try {
    const data = (await res.json()) as T;
    return { ok: true, data };
  } catch {
    console.error(`[catalogFetch] ${path}: failed to parse JSON`);
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }
}

// ── Virtual models ────────────────────────────────────────────────────────────

export async function fetchVirtualModels(
  accessToken: string,
): Promise<Result<{ data: VirtualModel[] }>> {
  return catalogFetch('/virtual-models', accessToken);
}

export async function fetchProviderModels(
  accessToken: string,
): Promise<Result<{ data: ProviderModel[] }>> {
  return catalogFetch('/provider-models', accessToken);
}
