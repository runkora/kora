import 'server-only';

import { config } from '@orbit/server/config';
import type { QuotaStatus } from '../_types';

type QuotaResult = { ok: true; data: QuotaStatus } | { ok: false; status: number; message: string };

export async function fetchPersonalQuotaFromGateway(accessToken: string): Promise<QuotaResult> {
  let res: Response;
  try {
    res = await fetch(`${config.inferenceGatewayUrl}/v1/usage/quota`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    console.error(`[fetchPersonalQuota] ${isTimeout ? 'timeout' : 'network error'}:`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Inference gateway timed out' : 'Failed to reach inference gateway',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[fetchPersonalQuota] responded ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  try {
    const data = (await res.json()) as QuotaStatus;
    return { ok: true, data };
  } catch {
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }
}
