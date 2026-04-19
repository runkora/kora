'use server';

import { getSession, isSessionValid } from '@orbit/server/session';
import {
  createPATInIdentity,
  revokePATInIdentity,
  updatePATScopesInIdentity,
} from '../_services/tokens-server';
import type {
  CreatePATResult,
  RevokePATResult,
  UpdatePATScopesResult,
} from '../_services/tokens-server';
import type { CreatePATRequest } from '@orbit/shared/api';

export async function createToken(req: CreatePATRequest): Promise<CreatePATResult> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }
  return createPATInIdentity(session.accessToken, req);
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export async function revokeToken(id: string): Promise<RevokePATResult> {
  if (!UUID_RE.test(id)) {
    return { ok: false, status: 400, message: 'Invalid token ID' };
  }
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }
  return revokePATInIdentity(session.accessToken, id);
}

export async function updateTokenScopes(
  id: string,
  scopes: string[],
): Promise<UpdatePATScopesResult> {
  if (!UUID_RE.test(id)) {
    return { ok: false, status: 400, message: 'Invalid token ID' };
  }
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }
  return updatePATScopesInIdentity(session.accessToken, id, { scopes });
}
