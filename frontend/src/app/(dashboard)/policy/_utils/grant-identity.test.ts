import { describe, expect, it } from 'vitest';
import type { Grant } from '../_types';
import { resolveGrantIdentity } from './grant-identity';

const agentId = 'spiffe://orbit.test/tenant/orbit-tenant-example/agent/ci-failure-investigator';

function grant(overrides: Partial<Grant>): Grant {
  return {
    id: 'g1',
    tenant_id: 'example',
    principal_type: 'AGENT',
    principal_id: agentId,
    scopes: [],
    created_at: '2026-09-29T00:00:00Z',
    ...overrides,
  };
}

describe('resolveGrantIdentity for agents', () => {
  it('uses the agent name as the primary text and keeps the full ID', () => {
    expect(resolveGrantIdentity(grant({}), {}, {})).toEqual({
      primary: 'ci-failure-investigator',
      rawId: agentId,
    });
  });

  it('falls back to the raw ID when it is not a SPIFFE ID', () => {
    expect(resolveGrantIdentity(grant({ principal_id: 'plain-id' }), {}, {})).toEqual({
      primary: 'plain-id',
    });
  });

  it('leaves SERVICE principals alone', () => {
    const service = grant({ principal_type: 'SERVICE', principal_id: agentId });
    expect(resolveGrantIdentity(service, {}, {}).primary).toBe(agentId);
  });
});
