import 'server-only';

import { config } from '@orbit/server/config';
import type { BudgetPrincipalsResponse } from '../_types';

type BudgetPrincipalsResult =
  { ok: true; data: BudgetPrincipalsResponse } | { ok: false; status: number; message: string };

export async function fetchBudgetPrincipalsFromGateway(
  accessToken: string,
): Promise<BudgetPrincipalsResult> {
  let res: Response;
  try {
    res = await fetch(`${config.inferenceGatewayUrl}/budgets/principals`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(15_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    console.error(`[fetchBudgetPrincipals] ${isTimeout ? 'timeout' : 'network error'}:`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Inference gateway timed out' : 'Failed to reach inference gateway',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[fetchBudgetPrincipals] responded ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  try {
    const data = (await res.json()) as BudgetPrincipalsResponse;
    return { ok: true, data };
  } catch {
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }
}
