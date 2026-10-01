import { describe, expect, it } from 'vitest';
import type { Grant } from '../_types';
import { agentNameFromPrincipalId, resolveGrantIdentity } from './grant-identity';

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

describe('agentNameFromPrincipalId', () => {
  it('takes the last path segment of a SPIFFE ID', () => {
    expect(agentNameFromPrincipalId(agentId)).toBe('ci-failure-investigator');
  });

  it('ignores a trailing slash', () => {
    expect(agentNameFromPrincipalId(`${agentId}/`)).toBe('ci-failure-investigator');
  });

  it('returns undefined when there is nothing after the trust domain', () => {
    expect(agentNameFromPrincipalId('spiffe://orbit.test')).toBeUndefined();
    expect(agentNameFromPrincipalId('spiffe://orbit.test/')).toBeUndefined();
  });

  it('returns undefined for an ID that is not a SPIFFE ID', () => {
    expect(agentNameFromPrincipalId('CgNkZXYSCmxvY2FsLWxkYXA')).toBeUndefined();
  });
});

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
