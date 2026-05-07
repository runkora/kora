import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Must mock before importing the module under test so the top-level
// `import { cookies } from 'next/headers'` does not throw in jsdom.
vi.mock('next/headers', () => ({
  cookies: vi.fn(),
}));

vi.mock('iron-session', () => ({
  getIronSession: vi.fn(),
}));

vi.mock('@orbit/server/auth-config', () => ({
  getSessionOptions: vi.fn().mockReturnValue({}),
}));

import { isSessionValid, isSessionExpiringSoon } from './session';

// ── Helpers ───────────────────────────────────────────────────────────────────

const BASE_EPOCH = 1_700_000_000; // arbitrary fixed epoch (seconds)

function makeSession(overrides: Record<string, unknown> = {}) {
  return {
    user: { sub: 'test-sub', email: 'test@orbit.local', name: 'Test' },
    accessToken: 'test-access-token',
    refreshToken: 'test-refresh',
    expiresAt: BASE_EPOCH + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: [], evaluatedAt: 0 },
    ...overrides,
  } as unknown as Parameters<typeof isSessionValid>[0];
}

// ── isSessionValid ─────────────────────────────────────────────────────────────

describe('isSessionValid', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(BASE_EPOCH * 1000);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns false when there is no user', () => {
    expect(isSessionValid(makeSession({ user: undefined }))).toBe(false);
  });

  it('returns false when there is no accessToken', () => {
    expect(isSessionValid(makeSession({ accessToken: undefined }))).toBe(false);
  });

  it('returns false when the token expired more than 30 seconds ago', () => {
    // expiresAt = BASE_EPOCH - 31 → nowSeconds - 30 = BASE_EPOCH - 30 → expiresAt < threshold
    expect(isSessionValid(makeSession({ expiresAt: BASE_EPOCH - 31 }))).toBe(false);
  });

  it('returns true at the boundary — token expired exactly 30 seconds ago (within drift tolerance)', () => {
    // expiresAt = BASE_EPOCH - 30 → threshold = BASE_EPOCH - 30 → expiresAt > threshold is false
    // The predicate is: expiresAt > nowSeconds - 30
    // With expiresAt = BASE_EPOCH - 30 and nowSeconds = BASE_EPOCH:
    //   BASE_EPOCH - 30 > BASE_EPOCH - 30 → false
    // So the boundary itself is NOT within tolerance (strictly greater-than).
    expect(isSessionValid(makeSession({ expiresAt: BASE_EPOCH - 30 }))).toBe(false);
  });

  it('returns true when the token expired 29 seconds ago (just inside drift tolerance)', () => {
    expect(isSessionValid(makeSession({ expiresAt: BASE_EPOCH - 29 }))).toBe(true);
  });

  it('returns true when the token expires in the future', () => {
    expect(isSessionValid(makeSession({ expiresAt: BASE_EPOCH + 3600 }))).toBe(true);
  });
});

// ── isSessionExpiringSoon ──────────────────────────────────────────────────────

describe('isSessionExpiringSoon', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(BASE_EPOCH * 1000);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns true (fail-safe) when expiresAt is undefined', () => {
    expect(isSessionExpiringSoon(makeSession({ expiresAt: undefined }))).toBe(true);
  });

  it('returns true when the token expires in less than 5 minutes', () => {
    // 4 min 59 s = 299 s remaining
    expect(isSessionExpiringSoon(makeSession({ expiresAt: BASE_EPOCH + 299 }))).toBe(true);
  });

  it('returns false at the boundary — token expires in exactly 5 minutes', () => {
    // 5 * 60 = 300 s → expiresAt - nowSeconds = 300 → 300 < 300 is false
    expect(isSessionExpiringSoon(makeSession({ expiresAt: BASE_EPOCH + 300 }))).toBe(false);
  });

  it('returns false when the token expires in more than 5 minutes', () => {
    expect(isSessionExpiringSoon(makeSession({ expiresAt: BASE_EPOCH + 600 }))).toBe(false);
  });
});
