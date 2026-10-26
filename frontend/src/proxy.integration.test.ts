/**
 * The inline token refresh in the proxy re-seals the session cookie. iron-session
 * is mocked to hand back the stored session and capture the re-sealed one; MSW
 * answers the identity service's refresh.
 */
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { http, HttpResponse } from 'msw';
import type { IronSession } from 'iron-session';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';
import {
  createBackendSessionResponse,
  createSessionPayload,
} from '@runkora/test-utils/factories/session';
import type { SessionPayload } from '@runkora/server/auth-config';

vi.mock('iron-session', () => ({
  getIronSession: vi.fn(),
}));

import { getIronSession } from 'iron-session';
import { proxy } from './proxy';

beforeAll(() => {
  vi.stubEnv('BFF_SESSION_SECRET', 'test-bff-secret-that-is-at-least-32-chars!');
  vi.stubEnv('KORA_API_URL', 'http://test-kora-api');
});

afterAll(() => {
  vi.unstubAllEnvs();
});

describe('proxy token refresh', () => {
  it('keeps a locked tenant locked', async () => {
    const stored = {
      ...createSessionPayload({ apiAuthoring: false }),
      expiresAt: Math.floor(Date.now() / 1000) + 60,
      save: vi.fn(),
      destroy: vi.fn(),
    };
    const resealed = { save: vi.fn().mockResolvedValue(undefined) } as Partial<SessionPayload> & {
      save: () => Promise<void>;
    };
    vi.mocked(getIronSession)
      .mockResolvedValueOnce(stored as unknown as IronSession<SessionPayload>)
      .mockResolvedValueOnce(resealed as unknown as IronSession<SessionPayload>);
    server.use(
      http.post('http://test-kora-api/identity/session', () =>
        HttpResponse.json(createBackendSessionResponse({ api_authoring: false })),
      ),
    );

    await proxy(new NextRequest('https://local.runkora.io/agents'));

    expect(resealed.save).toHaveBeenCalled();
    expect(resealed.apiAuthoring).toBe(false);
  });
});
