'use server';

import { getSession, isSessionValid } from '@runkora/server/session';
import { upsertTenantBudget, deleteTenantBudget } from '../_services/policy-server';
import type { TenantBudget } from '../_types/api';

type ActionResult<T> = { ok: true; data: T } | { ok: false; status: number; message: string };

export async function upsertTenantBudgetAction(
  limitUsd: number,
  resetCadence: string,
): Promise<ActionResult<TenantBudget>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  return upsertTenantBudget(session.accessToken, session.tenantId, limitUsd, resetCadence);
}

export async function deleteTenantBudgetAction(): Promise<ActionResult<void>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  return deleteTenantBudget(session.accessToken, session.tenantId);
}
