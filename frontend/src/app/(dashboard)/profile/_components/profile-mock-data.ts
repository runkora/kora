/**
 * Mock data for the profile page stories: the signed-in operator, their effective
 * permissions broken down by role and grant, and a mix of active and expired API keys.
 */

import type { IronSession } from 'iron-session';
import type { SessionPayload } from '@orbit/server/auth-config';
import type { ListPATsResult } from '@orbit/server/services/identity-server';
import type { PAT, ScopesExplainResponse } from '@orbit/shared/api';

const DAY = 24 * 60 * 60 * 1000;
const iso = (offsetMs: number): string => new Date(Date.now() + offsetMs).toISOString();

export const PRINCIPAL_ID = 'usr_42b7e9c1';
export const TENANT_ID = 'ten_8f3a21';

/** A sealed session as the page receives it, with no-op session methods. */
export const SESSION: IronSession<SessionPayload> = {
  user: { sub: PRINCIPAL_ID, email: 'dev@kora.example', name: 'Dev Operator' },
  accessToken: 'mock-access-token',
  refreshToken: 'mock-refresh-token',
  expiresAt: Math.floor(Date.now() / 1000) + 3600,
  tenantId: TENANT_ID,
  save: async () => {},
  destroy: () => {},
  updateConfig: () => {},
};

export const PROFILE = {
  ok: true as const,
  data: { id: PRINCIPAL_ID, email: 'dev@kora.example', display_name: 'Dev Operator' },
};

export const PROFILE_UNAVAILABLE = {
  ok: false as const,
  status: 503,
  message: 'identity service unavailable',
};

const ADMIN_SCOPES = [
  'platform:agents:read',
  'platform:agents:write',
  'platform:runs:read',
  'platform:runs:write',
  'platform:grants:read',
  'platform:roles:read',
  'platform:vocabulary:read',
];

const OPERATOR_SCOPES = ['platform:agents:read', 'platform:runs:read', 'inference:invoke'];

const GRANT_SCOPES = ['platform:triggers:read', 'platform:triggers:write', 'platform:budgets:read'];

export const EXPLAIN: ScopesExplainResponse = {
  principal_type: 'user',
  principal_id: PRINCIPAL_ID,
  tenant_id: TENANT_ID,
  from_roles: [
    { role: 'platform-admin', scopes: ADMIN_SCOPES },
    { role: 'agent-operator', scopes: OPERATOR_SCOPES },
  ],
  from_grants: [{ grant_id: 'grt_7c20e5a4', scopes: GRANT_SCOPES }],
  effective_scopes: [...new Set([...ADMIN_SCOPES, ...OPERATOR_SCOPES, ...GRANT_SCOPES])],
  evaluated_at: iso(0),
};

/** No roles or grants, so the "Granted by" card is absent and the scope set is empty. */
export const EXPLAIN_EMPTY: ScopesExplainResponse = {
  ...EXPLAIN,
  from_roles: [],
  from_grants: [],
  effective_scopes: [],
};

export const EXPLAIN_FAILED = {
  ok: false as const,
  status: 502,
  message: 'policy gateway unreachable',
};

const pat = (
  id: string,
  name: string,
  scopes: string[],
  createdAgo: number,
  expiresIn: number,
): PAT => ({
  id,
  name,
  scopes,
  created_at: iso(-createdAgo),
  expires_at: iso(expiresIn),
  revoked_at: null,
});

export const TOKENS: PAT[] = [
  pat(
    'pat_91d2a7',
    'ci-release-bot',
    ['platform:agents:read', 'platform:runs:write'],
    40 * DAY,
    50 * DAY,
  ),
  pat('pat_4be803', 'laptop-cli', ['inference:invoke', 'platform:agents:read'], 12 * DAY, 78 * DAY),
  pat('pat_c6f1d0', 'grafana-exporter', ['platform:runs:read'], 120 * DAY, -30 * DAY),
  pat('pat_0a77be', 'local-dev', [], 3 * DAY, 87 * DAY),
];

export const TOKENS_OK: ListPATsResult = { ok: true, tokens: TOKENS };
export const TOKENS_EMPTY: ListPATsResult = { ok: true, tokens: [] };
export const TOKENS_FAILED: ListPATsResult = {
  ok: false,
  status: 503,
  message: 'identity service unavailable',
};
