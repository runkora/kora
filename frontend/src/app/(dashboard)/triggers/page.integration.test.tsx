/**
 * The route is a server component, so it is just an async function returning an
 * element: calling it and reading the props it built is the whole test. What
 * matters is that the cursor survives the hop from the server fetch into the
 * shell, and that a rejected session does not render as "this tenant has no
 * deliveries", which is a different and much worse claim.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('server-only', () => ({}));
vi.mock('@orbit/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn(),
}));
vi.mock('./_services/triggers-server', () => ({
  fetchTriggersPage: vi.fn(),
  SESSION_EXPIRED_MESSAGE: 'Your session has expired.',
}));

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchTriggersPage } from './_services/triggers-server';
import TriggersPageRoute from './page';

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(getSession).mockResolvedValue({ accessToken: 'tok' } as never);
  vi.mocked(isSessionValid).mockReturnValue(true);
});

describe('Triggers route', () => {
  it('hands the cursor to the shell so Load more has a starting point', async () => {
    vi.mocked(fetchTriggersPage).mockResolvedValue({
      ok: true,
      sources: [],
      triggers: [],
      deliveries: [],
      deliveryCursor: '2026-10-07T12:00:00Z|d1',
      agents: [],
    });
    const el = await TriggersPageRoute();
    expect(el.props.initialDeliveryCursor).toBe('2026-10-07T12:00:00Z|d1');
    expect(el.props.initialError).toBeNull();
  });

  it('reports a rejected session rather than rendering an empty tenant', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    const el = await TriggersPageRoute();
    expect(el.props.initialError).toBe('Your session has expired.');
    expect(el.props.initialDeliveryCursor).toBeNull();
    expect(fetchTriggersPage).not.toHaveBeenCalled();
  });
});
