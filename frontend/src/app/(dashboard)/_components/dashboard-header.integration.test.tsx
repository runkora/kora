/**
 * Integration tests for DashboardHeader's mobile hamburger and dev-environment banner.
 *
 * `next/navigation` is mocked so `usePathname` is controllable. The header is
 * wrapped in a real `SidebarStateProvider` so the hamburger's wiring to the
 * shared open/close state is exercised end to end (toggle, aria-expanded, and
 * the accessible-label swap).
 *
 * `./permissions-provider`'s `useSession` is mocked so the dev-environment
 * banner tests can control `devEnvironmentId` without a live BFF round-trip
 * (mirrors dashboard-sidebar.integration.test.tsx's strategy).
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse, delay } from 'msw';
import { renderWithProviders, server } from '@orbit/test-utils';
import { SidebarStateProvider } from './sidebar-state';
import { DashboardHeader } from './dashboard-header';

vi.mock('next/navigation', () => ({
  usePathname: vi.fn().mockReturnValue('/'),
}));

vi.mock('./permissions-provider', () => ({
  useSession: vi.fn().mockReturnValue({
    scopes: [],
    user: null,
    loaded: true,
    devEnvironmentId: null,
  }),
}));

import { useSession } from './permissions-provider';

function renderHeader() {
  return renderWithProviders(
    <SidebarStateProvider>
      <DashboardHeader />
    </SidebarStateProvider>,
  );
}

describe('DashboardHeader hamburger', () => {
  it('renders the hamburger collapsed by default', () => {
    renderHeader();
    const button = screen.getByRole('button', { name: 'Open navigation' });
    expect(button).toHaveAttribute('aria-expanded', 'false');
  });

  it('opens the drawer state on click and swaps the accessible label', async () => {
    const user = userEvent.setup();
    renderHeader();

    await user.click(screen.getByRole('button', { name: 'Open navigation' }));

    const button = screen.getByRole('button', { name: 'Close navigation' });
    expect(button).toHaveAttribute('aria-expanded', 'true');
  });

  it('toggles back to closed on a second click', async () => {
    const user = userEvent.setup();
    renderHeader();

    await user.click(screen.getByRole('button', { name: 'Open navigation' }));
    await user.click(screen.getByRole('button', { name: 'Close navigation' }));

    expect(screen.getByRole('button', { name: 'Open navigation' })).toHaveAttribute(
      'aria-expanded',
      'false',
    );
  });
});

describe('DashboardHeader dev environment banner', () => {
  const EXIT_URL = '/api/dev-environment/exit';
  const mockLocation = { href: 'http://localhost/', origin: 'http://localhost', host: 'localhost' };

  beforeEach(() => {
    mockLocation.href = 'http://localhost/';
    vi.stubGlobal('location', mockLocation);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders no banner or button when devEnvironmentId is null', () => {
    vi.mocked(useSession).mockReturnValue({
      scopes: [],
      user: null,
      loaded: true,
      devEnvironmentId: null,
    });
    renderHeader();
    expect(screen.queryByText(/Dev Env:/)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Return to Orbit/ })).not.toBeInTheDocument();
  });

  it('renders "Dev Env: {id}" and a "Return to Orbit" button when devEnvironmentId is set', () => {
    vi.mocked(useSession).mockReturnValue({
      scopes: [],
      user: null,
      loaded: true,
      devEnvironmentId: 'bob-env',
    });
    renderHeader();
    expect(screen.getByText(/Dev Env:/)).toBeInTheDocument();
    expect(screen.getByText('bob-env')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Return to Orbit/ })).toBeInTheDocument();
  });

  it('clicking "Return to Orbit" calls the exit route and navigates to /', async () => {
    vi.mocked(useSession).mockReturnValue({
      scopes: [],
      user: null,
      loaded: true,
      devEnvironmentId: 'bob-env',
    });
    // Returns a plain 200, not the real route's 307 — the redirect itself is
    // the route handler's own concern (frontend/src/app/api/dev-environment/exit/route.ts);
    // this test only verifies DashboardHeader hits the endpoint and then forces
    // a full navigation. A mocked redirect would make the fetch client try to
    // follow it to an unhandled "/" request in this test environment.
    let exitCalled = false;
    server.use(
      http.get(EXIT_URL, () => {
        exitCalled = true;
        return new HttpResponse(null, { status: 200 });
      }),
    );

    renderHeader();
    await userEvent.click(screen.getByRole('button', { name: /Return to Orbit/ }));

    await waitFor(() => {
      expect(exitCalled).toBe(true);
      expect(mockLocation.href).toBe('/');
    });
  });

  it('disables the "Return to Orbit" button while the request is in-flight', async () => {
    vi.mocked(useSession).mockReturnValue({
      scopes: [],
      user: null,
      loaded: true,
      devEnvironmentId: 'bob-env',
    });
    server.use(
      http.get(EXIT_URL, async () => {
        await delay('infinite');
        return new HttpResponse(null, { status: 200 });
      }),
    );

    renderHeader();
    await userEvent.click(screen.getByRole('button', { name: /Return to Orbit/ }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Return to Orbit/ })).toBeDisabled();
    });
  });
});
