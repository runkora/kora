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

describe('resolveGrantIdentity for humans', () => {
  const humanId = 'CgNkZXYSCmxvY2FsLWxkYXA';
  const human = grant({ principal_type: 'HUMAN', principal_id: humanId });

  it('uses the display name, with the email beneath it', () => {
    const user = { id: humanId, email: 'dev@example.com', display_name: 'Dev' };
    expect(resolveGrantIdentity(human, {}, { [humanId]: user })).toEqual({
      primary: 'Dev',
      email: 'dev@example.com',
      rawId: humanId,
    });
  });

  it('uses the email alone when there is no display name', () => {
    const user = { id: humanId, email: 'dev@example.com', display_name: '' };
    expect(resolveGrantIdentity(human, {}, { [humanId]: user })).toEqual({
      primary: 'dev@example.com',
      email: undefined,
      rawId: humanId,
    });
  });

  it('falls back to the raw ID when the user is unknown', () => {
    expect(resolveGrantIdentity(human, {}, {})).toEqual({ primary: humanId });
  });
});

describe('resolveGrantIdentity for PATs', () => {
  const patId = 'pat-123';
  const pat = grant({ principal_type: 'PAT', principal_id: patId });

  it('uses the token name and names its owner', () => {
    const owner = { id: 'u1', email: 'owner@example.com', display_name: 'Owner' };
    expect(resolveGrantIdentity(pat, { [patId]: { pat_name: 'ci-token', owner } }, {})).toEqual({
      primary: 'ci-token',
      ownerName: 'Owner',
      ownerEmail: 'owner@example.com',
      rawId: patId,
    });
  });

  it('shows the owner by email alone when they have no display name', () => {
    const owner = { id: 'u1', email: 'owner@example.com', display_name: '' };
    expect(resolveGrantIdentity(pat, { [patId]: { pat_name: 'ci-token', owner } }, {})).toEqual({
      primary: 'ci-token',
      ownerName: 'owner@example.com',
      ownerEmail: undefined,
      rawId: patId,
    });
  });

  it('has no owner when attribution does not name one', () => {
    expect(resolveGrantIdentity(pat, { [patId]: { pat_name: 'ci-token' } }, {})).toEqual({
      primary: 'ci-token',
      ownerName: undefined,
      ownerEmail: undefined,
      rawId: patId,
    });
  });

  it('falls back to the raw ID when the token is unknown', () => {
    expect(resolveGrantIdentity(pat, {}, {})).toEqual({
      primary: patId,
      ownerName: undefined,
      ownerEmail: undefined,
      rawId: patId,
    });
  });
});
