import { afterEach, describe, expect, it, vi } from 'vitest';

const { signinRedirect } = vi.hoisted(() => ({ signinRedirect: vi.fn() }));

vi.mock('@runkora/lib/oidc-client', () => ({
  TENANT_CONFIG_STORAGE_KEY: 'tenant-config',
  buildUserManager: () => ({ signinRedirect }),
}));

import { startTenantSwitch } from './tenant-switch';

const VALID = {
  tenant_id: 'kora-tenant-example',
  issuer_url: 'https://auth.example.com',
  client_id: 'dashboard',
  scopes: ['openid', 'email'],
};

function discoverReturns(body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json(body)));
}

const SWITCH = { email: 'dev@example.com', tenantId: 'kora-tenant-example-data', returnPath: '/' };

afterEach(() => {
  vi.unstubAllGlobals();
  signinRedirect.mockReset();
});

describe('startTenantSwitch', () => {
  it('redirects with the discovered issuer and client', async () => {
    discoverReturns(VALID);
    await startTenantSwitch(SWITCH);
    expect(signinRedirect).toHaveBeenCalledWith(
      expect.objectContaining({ prompt: 'none', login_hint: 'dev@example.com' }),
    );
  });

  it.each([
    ['no issuer', { ...VALID, issuer_url: undefined }],
    ['an empty client', { ...VALID, client_id: '' }],
    ['no scopes', { ...VALID, scopes: undefined }],
    ['a null body', null],
  ])('rejects a discovery answer with %s and does not redirect', async (_, body) => {
    discoverReturns(body);
    await expect(startTenantSwitch(SWITCH)).rejects.toThrow('incomplete config');
    expect(signinRedirect).not.toHaveBeenCalled();
  });
});
