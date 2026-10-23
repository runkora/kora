'use server';

import { getSession, isSessionValid } from '@runkora/server/session';
import { getRole, upsertRole, deleteRole, updateRoleBudget } from '../_services/policy-server';
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

/**
 * Creates a role, refusing a name that is already taken. The upsert behind `upsertRoleAction`
 * replaces an existing role of the same name, so this looks the name up first and fails closed:
 * it saves only on a definite "not found", never on a failed lookup. Names match exactly, as
 * they do in the policy service.
 */
export async function createRoleAction(req: UpsertRoleRequest): Promise<ActionResult<RoleEntry>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }
  const tenantId = req.tenant_id ?? session.tenantId;

  const existing = await getRole(session.accessToken, tenantId, req.role);
  if (existing.ok) {
    return { ok: false, status: 409, message: `A role named "${req.role}" already exists.` };
  }
  if (existing.status !== 404) {
    return {
      ok: false,
      status: existing.status,
      message: 'Could not check whether this role name is free. Try again.',
    };
  }

  return upsertRole(session.accessToken, { ...req, tenant_id: tenantId });
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
