import { describe, expect, it } from 'vitest';

import {
  declarativeRefusal,
  isDeclared,
  PLATFORM_LOCKED_MESSAGE,
  repositoryUrl,
  writeErrorMessage,
} from './declarative';

describe('isDeclared', () => {
  it('is true only for crd items', () => {
    expect(isDeclared({ source: 'crd' })).toBe(true);
    expect(isDeclared({ source: 'api' })).toBe(false);
    expect(isDeclared({})).toBe(false);
    expect(isDeclared(null)).toBe(false);
  });
});

describe('declarativeRefusal', () => {
  it('names the configuration repository for both refusal codes', () => {
    expect(declarativeRefusal('managed_by_resource')).toMatch(/item is managed in git/);
    expect(declarativeRefusal('api_authoring_disabled')).toMatch(/tenant is managed in git/);
  });

  it('is null for any other code', () => {
    expect(declarativeRefusal('conflict')).toBeNull();
    expect(declarativeRefusal(undefined)).toBeNull();
  });
});

describe('writeErrorMessage', () => {
  it('prefers the managed-in-git message for a refusal code', () => {
    expect(
      writeErrorMessage(409, { code: 'managed_by_resource', message: 'upstream text' }),
    ).toMatch(/managed in git/);
  });

  it('names the platform switch for a virtual model write', () => {
    const body = { code: 'api_authoring_disabled' };
    expect(writeErrorMessage(403, body, { platform: true })).toBe(PLATFORM_LOCKED_MESSAGE);
    expect(writeErrorMessage(403, body)).toMatch(/This tenant is managed in git/);
  });

  it('falls back to the API message, then the status', () => {
    expect(writeErrorMessage(400, { code: 'invalid', message: 'bad name' })).toBe('bad name');
    expect(writeErrorMessage(500, null)).toBe('Request failed (500)');
  });
});

describe('repositoryUrl', () => {
  it.each([
    ['github.com/acme/agents', 'https://github.com/acme/agents'],
    ['https://gitlab.example.com/acme/agents', 'https://gitlab.example.com/acme/agents'],
    ['payments-agents', null],
    ['javascript:alert(1)', null],
    ['http://github.com/acme/agents', null],
  ])('%s', (repository, want) => {
    expect(repositoryUrl(repository)).toBe(want);
  });
});
