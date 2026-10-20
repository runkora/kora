'use server';

import { getSession, isSessionValid } from '@runkora/server/session';
import { upsertRole, deleteRole, updateRoleBudget } from '../_services/policy-server';
import type { BudgetAssignment, UpsertRoleRequest, RoleEntry } from '../_types/api';

type ActionResult<T> = { ok: true; data: T } | { ok: false; status: number; message: string };

export async function upsertRoleAction(req: UpsertRoleRequest): Promise<ActionResult<RoleEntry>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  return upsertRole(session.accessToken, {
    ...req,
    tenant_id: req.tenant_id ?? session.tenantId,
  });
}

export async function updateRoleBudgetAction(
  role: string,
  budget: BudgetAssignment | null,
): Promise<ActionResult<RoleEntry>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  return updateRoleBudget(session.accessToken, session.tenantId, role, budget);
}

export async function deleteRoleAction(role: string): Promise<ActionResult<void>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  return deleteRole(session.accessToken, session.tenantId, role);
}
