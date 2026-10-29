import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { getSessionOptions, SESSION_COOKIE_NAME } from './auth-config';

describe('getSessionOptions', () => {
  const originalEnv = process.env;

  beforeEach(() => {
    process.env = { ...originalEnv };
  });

  afterEach(() => {
    process.env = originalEnv;
  });

  it('throws when BFF_SESSION_SECRET is absent', () => {
    delete process.env.BFF_SESSION_SECRET;
    expect(() => getSessionOptions()).toThrow('BFF_SESSION_SECRET must be set');
  });

  it('throws when BFF_SESSION_SECRET is shorter than 32 characters', () => {
    process.env.BFF_SESSION_SECRET = 'short';
    expect(() => getSessionOptions()).toThrow('BFF_SESSION_SECRET must be set');
  });

  it('does not throw when BFF_SESSION_SECRET is exactly 32 characters', () => {
    process.env.BFF_SESSION_SECRET = 'a'.repeat(32);
    expect(() => getSessionOptions()).not.toThrow();
  });

  it('uses refreshTokenExpiresIn as maxAge when truthy', () => {
    process.env.BFF_SESSION_SECRET = 'a'.repeat(32);
    const opts = getSessionOptions(7200);
    expect(opts.cookieOptions?.maxAge).toBe(7200);
  });

  it('falls back to 30 days when refreshTokenExpiresIn is absent', () => {
    process.env.BFF_SESSION_SECRET = 'a'.repeat(32);
    const opts = getSessionOptions();
    expect(opts.cookieOptions?.maxAge).toBe(60 * 60 * 24 * 30);
  });

  it('falls back to 30 days when refreshTokenExpiresIn is 0', () => {
    process.env.BFF_SESSION_SECRET = 'a'.repeat(32);
    const opts = getSessionOptions(0);
    expect(opts.cookieOptions?.maxAge).toBe(60 * 60 * 24 * 30);
  });

  it('uses SESSION_COOKIE_NAME as the cookieName', () => {
    process.env.BFF_SESSION_SECRET = 'a'.repeat(32);
    const opts = getSessionOptions();
    expect(opts.cookieName).toBe(SESSION_COOKIE_NAME);
  });

  describe('cookie secure flag', () => {
    const secureFor = (nodeEnv: string, appUrl: string | undefined) => {
      process.env.BFF_SESSION_SECRET = 'a'.repeat(32);
      Object.assign(process.env, { NODE_ENV: nodeEnv });
      if (appUrl === undefined) delete process.env.NEXT_PUBLIC_APP_URL;
      else process.env.NEXT_PUBLIC_APP_URL = appUrl;
      return getSessionOptions().cookieOptions?.secure;
    };

    it('keeps Secure on an https host', () => {
      expect(secureFor('production', 'https://local.runkora.io:8443')).toBe(true);
    });

    it('keeps Secure on https://kora.localhost', () => {
      expect(secureFor('production', 'https://kora.localhost')).toBe(true);
    });

    it('drops Secure on http://localhost', () => {
      expect(secureFor('production', 'http://localhost:9880')).toBe(false);
    });

    it('drops Secure on http://kora.localhost', () => {
      expect(secureFor('production', 'http://kora.localhost:9880')).toBe(false);
    });

    it('keeps Secure on http with a non-loopback host', () => {
      expect(secureFor('production', 'http://example.com')).toBe(true);
    });

    it('keeps Secure on a host that only ends in localhost', () => {
      expect(secureFor('production', 'http://notlocalhost')).toBe(true);
    });

    it('keeps Secure when the app URL is unset or invalid', () => {
      expect(secureFor('production', undefined)).toBe(true);
      expect(secureFor('production', 'not a url')).toBe(true);
    });

    it('stays non-Secure outside production', () => {
      expect(secureFor('development', 'https://example.com')).toBe(false);
    });
  });
});
