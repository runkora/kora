/**
 * Integration tests for LogoutButton.
 *
 * Strategy:
 * - `window.location` is replaced with a plain object so that href assignment
 *   can be observed without triggering a real navigation in jsdom.
 * - MSW intercepts POST /api/auth/logout for request capture and error states.
 * - `delay('infinite')` keeps the fetch pending to verify the in-flight state.
 */
import { describe, it, expect, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse, delay } from 'msw';
import { renderWithProviders, server } from '@orbit/test-utils';
import { LogoutButton } from './logout-button';

// ── Location mock ─────────────────────────────────────────────────────────────
//
// Replace window.location with a plain writable object so href assignment can
// be observed. The initial href must be a valid base URL because MSW's Node
// fetch interceptor calls `new URL(relativeUrl, window.location.href)` to
// resolve relative paths — an empty base throws ERR_INVALID_URL.

const mockLocation = { href: 'http://localhost/', origin: 'http://localhost' };

beforeEach(() => {
  mockLocation.href = 'http://localhost/';
  Object.defineProperty(window, 'location', { value: mockLocation, writable: true });
});

// ── Helpers ───────────────────────────────────────────────────────────────────

const LOGOUT_URL = '/api/auth/logout';

function renderButton() {
  return renderWithProviders(<LogoutButton />);
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('LogoutButton', () => {
  it('renders a button with aria-label="Logout"', () => {
    renderButton();
    expect(screen.getByRole('button', { name: 'Logout' })).toBeInTheDocument();
  });

  it('clicking calls POST /api/auth/logout', async () => {
    let logoutCalled = false;
    server.use(
      http.post(LOGOUT_URL, () => {
        logoutCalled = true;
        return new HttpResponse(null, { status: 200 });
      }),
    );

    renderButton();
    await userEvent.click(screen.getByRole('button', { name: 'Logout' }));
    await waitFor(() => {
      expect(logoutCalled).toBe(true);
    });
  });

  it('redirects to /login after a successful logout', async () => {
    server.use(http.post(LOGOUT_URL, () => new HttpResponse(null, { status: 200 })));

    renderButton();
    await userEvent.click(screen.getByRole('button', { name: 'Logout' }));
    await waitFor(() => {
      expect(mockLocation.href).toBe('/login');
    });
  });

  it('redirects to /login even when the logout endpoint returns 500', async () => {
    server.use(http.post(LOGOUT_URL, () => new HttpResponse(null, { status: 500 })));

    renderButton();
    await userEvent.click(screen.getByRole('button', { name: 'Logout' }));
    await waitFor(() => {
      expect(mockLocation.href).toBe('/login');
    });
  });

  it('disables the button while the request is in-flight', async () => {
    server.use(
      http.post(LOGOUT_URL, async () => {
        await delay('infinite');
        return new HttpResponse(null, { status: 200 });
      }),
    );

    renderButton();
    await userEvent.click(screen.getByRole('button', { name: 'Logout' }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Logout' })).toBeDisabled();
    });
  });
});
