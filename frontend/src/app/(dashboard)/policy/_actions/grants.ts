'use server';

import { getSession, isSessionValid } from '@orbit/server/session';
import { createGrant, deleteGrant } from '../_services/policy-server';
import type { CreateGrantRequest, Grant } from '../_types/api';

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

export async function deleteGrantAction(id: string): Promise<ActionResult<void>> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }

  return deleteGrant(session.accessToken, id);
}
