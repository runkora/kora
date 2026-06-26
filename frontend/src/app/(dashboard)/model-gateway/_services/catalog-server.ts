import 'server-only';

/**
 * Server-side catalog data fetching for RSC (React Server Component) initial load.
 *
 * Architecture note — RSC load vs client mutations:
 *   - Initial data (page load): fetched here using the server session token via
 *     the direct catalogFetch helper. Results are passed as props into the
 *     CatalogTab client component.
 *   - Subsequent mutations (create, update, delete, toggle): performed by client
 *     components via the Next.js API route layer at /api/catalog/*, which also
 *     holds the session token server-side. This keeps the access token out of
 *     the browser while still allowing interactive updates without a full page
 *     reload.
 *
 * If the initial load is needed at the component level (e.g. for Suspense),
 * move the fetch calls into a loader function co-located with the page.
 */

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

export interface VirtualModelPricingWire {
  effective_input_cost_per_1m: number;
  effective_output_cost_per_1m: number;
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
  pricing?: VirtualModelPricingWire;
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
  pricing?: PricingEntryWire;
}

export interface PricingEntryWire {
  provider: string;
  model_id: string;
  synced_input_cost_per_1m: number;
  synced_output_cost_per_1m: number;
  synced_cache_read_cost_per_1m: number;
  synced_cache_write_cost_per_1m: number;
  override_input_cost_per_1m: number | null;
  override_output_cost_per_1m: number | null;
  override_cache_read_cost_per_1m: number | null;
  override_cache_write_cost_per_1m: number | null;
  effective_input_cost_per_1m: number;
  effective_output_cost_per_1m: number;
  has_override: boolean;
  last_synced_at: string; // RFC3339
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

export async function fetchPricing(
  accessToken: string,
): Promise<Result<{ data: PricingEntryWire[] }>> {
  return catalogFetch('/pricing', accessToken);
}
