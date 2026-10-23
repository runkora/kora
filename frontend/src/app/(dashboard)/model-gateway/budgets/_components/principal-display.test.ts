import { describe, expect, it } from 'vitest';
import { principalDisplay } from './principal-display';

const agentId = 'spiffe://runkora.test/tenant/kora-tenant-example/agent/ci-failure-investigator';

describe('principalDisplay', () => {
  it('shows an agent by the name at the end of its SPIFFE ID and keeps the full ID', () => {
    expect(principalDisplay(agentId, 'AGENT', null)).toEqual({
      primary: 'ci-failure-investigator',
      secondary: agentId,
    });
  });

  it('does not parse the ID of a principal that is not an agent', () => {
    expect(principalDisplay(agentId, 'SERVICE', null)).toEqual({
      primary: agentId,
      secondary: null,
    });
  });

  it('shows a human by display name, then email', () => {
    expect(
      principalDisplay('abc', 'HUMAN', { display_name: 'Dev', email: 'dev@example.com' }),
    ).toEqual({ primary: 'Dev', secondary: 'dev@example.com' });
    expect(
      principalDisplay('abc', 'HUMAN', { display_name: '', email: 'dev@example.com' }),
    ).toEqual({ primary: 'dev@example.com', secondary: 'dev@example.com' });
  });

  it('falls back to the raw ID', () => {
    expect(principalDisplay('abc', 'HUMAN', undefined)).toEqual({
      primary: 'abc',
      secondary: null,
    });
  });
});
