/**
 * Integration tests for PermissionsProvider.
 *
 * Strategy:
 * - Renders `<PermissionsProvider>` with a Consumer child that exposes context
 *   values as data-testid elements.
 * - MSW controls GET /api/auth/session responses.
 * - The default auth handler (from test-utils) returns { user, expiresAt }
 *   without permissions; tests that need scopes override with `server.use(...)`.
 */
import { describe, it, expect } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils';
import { createSessionUser } from '@orbit/test-utils/factories/session';
import { Text } from '@orbit/terra';
import { PermissionsProvider, useSession } from './permissions-provider';

// ── Test consumer ─────────────────────────────────────────────────────────────

function Consumer() {
  const { scopes, user, loaded } = useSession();
  return (
    <>
      <Text data-testid="loaded">{String(loaded)}</Text>
      <Text data-testid="scopes">{scopes.join(',')}</Text>
      <Text data-testid="user-email">{user?.email ?? 'null'}</Text>
    </>
  );
}

function renderProvider() {
  return render(
    <PermissionsProvider>
      <Consumer />
    </PermissionsProvider>,
  );
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('PermissionsProvider', () => {
  it('loaded starts as false, becomes true after the fetch resolves', async () => {
    renderProvider();
    expect(screen.getByTestId('loaded').textContent).toBe('false');
    await waitFor(() => {
      expect(screen.getByTestId('loaded').textContent).toBe('true');
    });
  });

  it('extracts scopes from data.permissions.scopes in the response', async () => {
    const user = createSessionUser();
    server.use(
      http.get('/api/auth/session', () =>
        HttpResponse.json({
          user,
          expiresAt: Math.floor(Date.now() / 1000) + 3600,
          permissions: { scopes: ['platform:grants:read', 'platform:grants:write'] },
        }),
      ),
    );

    renderProvider();
    await waitFor(() => {
      expect(screen.getByTestId('scopes').textContent).toBe(
        'platform:grants:read,platform:grants:write',
      );
    });
  });

  it('extracts user from data.user in the response', async () => {
    const user = createSessionUser({ email: 'alice@orbit.local', name: 'Alice' });
    server.use(
      http.get('/api/auth/session', () =>
        HttpResponse.json({
          user,
          expiresAt: Math.floor(Date.now() / 1000) + 3600,
        }),
      ),
    );

    renderProvider();
    await waitFor(() => {
      expect(screen.getByTestId('user-email').textContent).toBe('alice@orbit.local');
    });
  });

  it('fails open: scopes = [], loaded = true when session returns non-ok (401)', async () => {
    server.use(http.get('/api/auth/session', () => new HttpResponse(null, { status: 401 })));

    renderProvider();
    await waitFor(() => {
      expect(screen.getByTestId('loaded').textContent).toBe('true');
    });
    expect(screen.getByTestId('scopes').textContent).toBe('');
    expect(screen.getByTestId('user-email').textContent).toBe('null');
  });

  it('fails open: scopes = [], loaded = true when fetch throws a network error', async () => {
    server.use(http.get('/api/auth/session', () => HttpResponse.error()));

    renderProvider();
    await waitFor(() => {
      expect(screen.getByTestId('loaded').textContent).toBe('true');
    });
    expect(screen.getByTestId('scopes').textContent).toBe('');
    expect(screen.getByTestId('user-email').textContent).toBe('null');
  });

  it('does not call the session endpoint more than once on re-render', async () => {
    let callCount = 0;
    server.use(
      http.get('/api/auth/session', () => {
        callCount += 1;
        return HttpResponse.json({
          user: createSessionUser(),
          expiresAt: Math.floor(Date.now() / 1000) + 3600,
        });
      }),
    );

    const { rerender } = renderProvider();

    await waitFor(() => {
      expect(screen.getByTestId('loaded').textContent).toBe('true');
    });

    rerender(
      <PermissionsProvider>
        <Consumer />
      </PermissionsProvider>,
    );

    expect(callCount).toBe(1);
  });
});
