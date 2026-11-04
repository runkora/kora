'use server';

import { getSession, isSessionValid } from '@runkora/server/session';
import { createGrant, deleteGrant, updateGrantBudget } from '../_services/policy-server';
import { searchUsers } from '../../model-gateway/_services/tokens-server';
import type { PATAttributionEntry } from '../../model-gateway/_services/tokens-server';
import type { BudgetAssignment, CreateGrantRequest, Grant } from '../_types/api';

type ActionResult<T> = { ok: true; data: T } | { ok: false; status: number; message: string };

export async function createGrantAction(req: CreateGrantRequest): Promise<ActionResult<Grant>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  return createGrant(session.accessToken, {
    ...req,
    tenant_id: req.tenant_id ?? session.tenantId,
  });
}

export async function updateGrantBudgetAction(
  id: string,
  budget: BudgetAssignment | null,
): Promise<ActionResult<Grant>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  return updateGrantBudget(session.accessToken, id, budget);
}

export async function deleteGrantAction(id: string): Promise<ActionResult<void>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  return deleteGrant(session.accessToken, id);
}

export async function searchUsersAction(
  query: string,
): Promise<ActionResult<PATAttributionEntry[]>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  const result = await searchUsers(session.accessToken, query, 20);
  if (!result.ok) {
    return { ok: false, status: result.status, message: result.message };
  }
  return { ok: true, data: result.users };
}
