import { describe, it, expect, beforeAll } from 'vitest';
import { mcpConsentToken, consentTokenValid } from './mcp-consent';

const input = {
  userKey: 'jane@acme.com',
  clientId: 'cid-123',
  redirectUri: 'http://127.0.0.1:9000/callback',
  codeChallenge: 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM',
};

describe('mcpConsentToken / consentTokenValid', () => {
  beforeAll(() => {
    process.env.BFF_SESSION_SECRET = 'x'.repeat(32);
  });

  it('is deterministic for identical inputs', () => {
    expect(mcpConsentToken(input)).toBe(mcpConsentToken(input));
  });

  it('validates a matching token', () => {
    expect(consentTokenValid(mcpConsentToken(input), input)).toBe(true);
  });

  it('rejects a missing token', () => {
    expect(consentTokenValid(null, input)).toBe(false);
    expect(consentTokenValid('', input)).toBe(false);
  });

  it('rejects a forged/garbage token', () => {
    expect(consentTokenValid('not-a-real-token', input)).toBe(false);
  });

  it('rejects when any bound value differs (user or OAuth params)', () => {
    const token = mcpConsentToken(input);
    expect(consentTokenValid(token, { ...input, userKey: 'mallory@acme.com' })).toBe(false);
    expect(consentTokenValid(token, { ...input, clientId: 'other-client' })).toBe(false);
    expect(consentTokenValid(token, { ...input, redirectUri: 'http://127.0.0.1:1/x' })).toBe(false);
    expect(consentTokenValid(token, { ...input, codeChallenge: 'different' })).toBe(false);
  });
});
