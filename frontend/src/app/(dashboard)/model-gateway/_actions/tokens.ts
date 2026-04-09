'use server';

import { getSession, isSessionValid } from '@orbit/server/session';
import { createPATInIdentity, revokePATInIdentity } from '../_services/tokens-server';
import type { CreatePATResult, RevokePATResult } from '../_services/tokens-server';
import type { CreatePATRequest } from '@orbit/shared/api';

export async function createToken(req: CreatePATRequest): Promise<CreatePATResult> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }
  return createPATInIdentity(session.accessToken, req);
}

export async function revokeToken(id: string): Promise<RevokePATResult> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return { ok: false, status: 401, message: 'Unauthorized' };
  }
  return revokePATInIdentity(session.accessToken, id);
}
