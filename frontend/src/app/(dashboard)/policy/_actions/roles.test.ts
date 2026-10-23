import { beforeEach, describe, expect, it, vi } from 'vitest';

const getSession = vi.fn();
const getRole = vi.fn();
const upsertRole = vi.fn();

vi.mock('@runkora/server/session', () => ({
  getSession: () => getSession(),
  isSessionValid: (s: { accessToken?: string } | null) => !!s?.accessToken,
}));
vi.mock('../_services/policy-server', () => ({
  getRole: (...args: unknown[]) => getRole(...args),
  upsertRole: (...args: unknown[]) => upsertRole(...args),
  deleteRole: vi.fn(),
  updateRoleBudget: vi.fn(),
}));

import { createRoleAction } from './roles';

const REQ = { role: 'ml-engineer', scopes: ['agents:read'], budget: null };

describe('createRoleAction', () => {
  beforeEach(() => {
    getSession.mockReset().mockResolvedValue({ accessToken: 'tok', tenantId: 't1' });
    getRole.mockReset();
    upsertRole.mockReset();
  });

  it('rejects an unauthenticated caller without looking anything up', async () => {
    getSession.mockResolvedValue(null);
    expect(await createRoleAction(REQ)).toMatchObject({ ok: false, status: 401 });
    expect(getRole).not.toHaveBeenCalled();
    expect(upsertRole).not.toHaveBeenCalled();
  });

  it('saves only when the lookup says the role does not exist', async () => {
    getRole.mockResolvedValue({ ok: false, status: 404, message: 'role not found' });
    upsertRole.mockResolvedValue({ ok: true, data: { role: 'ml-engineer' } });
    const result = await createRoleAction(REQ);
    expect(getRole).toHaveBeenCalledWith('tok', 't1', 'ml-engineer');
    expect(upsertRole).toHaveBeenCalledWith('tok', { ...REQ, tenant_id: 't1' });
    expect(result.ok).toBe(true);
  });

  it('refuses a name that already exists, including a reserved role, and never saves', async () => {
    getRole.mockResolvedValue({ ok: true, data: { role: 'ml-engineer', scopes: [] } });
    expect(await createRoleAction(REQ)).toEqual({
      ok: false,
      status: 409,
      message: 'A role named "ml-engineer" already exists.',
    });
    expect(upsertRole).not.toHaveBeenCalled();
  });

  it.each([500, 502, 403])('fails closed when the lookup fails with %i', async (status) => {
    getRole.mockResolvedValue({ ok: false, status, message: 'boom' });
    expect(await createRoleAction(REQ)).toMatchObject({ ok: false, status });
    expect(upsertRole).not.toHaveBeenCalled();
  });
});
