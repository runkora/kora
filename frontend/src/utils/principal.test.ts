import { describe, expect, it } from 'vitest';
import { agentNameFromPrincipalId } from './principal';

const agentId = 'spiffe://orbit.test/tenant/kora-tenant-example/agent/ci-failure-investigator';

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
