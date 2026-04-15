'use server';

import { getSession, isSessionValid } from '@orbit/server/session';
import { upsertRole, deleteRole } from '../_services/policy-server';
import type { UpsertRoleRequest, RoleEntry } from '../_types/api';

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

export async function deleteRoleAction(role: string): Promise<ActionResult<void>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  return deleteRole(session.accessToken, session.tenantId, role);
}
