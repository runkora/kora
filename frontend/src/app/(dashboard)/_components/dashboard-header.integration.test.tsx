/**
 * Integration tests for DashboardHeader's mobile hamburger.
 *
 * `next/navigation` is mocked so `usePathname` is controllable. The header is
 * wrapped in a real `SidebarStateProvider` so the hamburger's wiring to the
 * shared open/close state is exercised end to end (toggle, aria-expanded, and
 * the accessible-label swap).
 */
import { afterEach, describe, it, expect, vi } from 'vitest';
import { act, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@runkora/test-utils';
import { startTenantSwitch } from '@runkora/lib/tenant-switch';
import { PermissionsProvider } from './permissions-provider';
import { SidebarStateProvider } from './sidebar-state';
import { DashboardHeader } from './dashboard-header';

vi.mock('next/navigation', () => ({
  usePathname: vi.fn().mockReturnValue('/'),
}));

vi.mock('@runkora/lib/tenant-switch', () => ({ startTenantSwitch: vi.fn() }));

function renderHeader() {
  return renderWithProviders(
    <SidebarStateProvider>
      <DashboardHeader />
    </SidebarStateProvider>,
  );
}

describe('DashboardHeader version badge', () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it('shows the version the image was built for, beside the theme toggle', () => {
    vi.stubEnv('NEXT_PUBLIC_KORA_VERSION', 'v0.5.0-rc.1');
    renderHeader();
    expect(screen.getByText('v0.5.0-rc.1')).toBeInTheDocument();
  });

  it('shows dev when no version was built in', () => {
    vi.stubEnv('NEXT_PUBLIC_KORA_VERSION', undefined);
    renderHeader();
    expect(screen.getByText('dev')).toBeInTheDocument();
  });
});

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

// The default MSW session carries two tenants, so the switcher renders from the real fetch.
describe('DashboardHeader tenant switcher', () => {
  function renderWithSession() {
    return renderWithProviders(
      <PermissionsProvider>
        <SidebarStateProvider>
          <DashboardHeader />
        </SidebarStateProvider>
      </PermissionsProvider>,
    );
  }

  it('names the active tenant on the trigger', async () => {
    renderWithSession();
    expect(
      await screen.findByRole('button', { name: 'Example Platform, switch tenant' }),
    ).toBeInTheDocument();
  });

  it('starts a switch to the chosen tenant from the current path', async () => {
    vi.mocked(startTenantSwitch).mockResolvedValue();
    const user = userEvent.setup();
    renderWithSession();

    await user.click(
      await screen.findByRole('button', { name: 'Example Platform, switch tenant' }),
    );
    await user.click(await screen.findByRole('button', { name: 'Example Data' }));

    expect(startTenantSwitch).toHaveBeenCalledWith({
      email: 'dev@runkora.test',
      tenantId: 'example-data',
      returnPath: '/',
    });
  });

  it('shows an error and re-enables the list when the switch cannot start', async () => {
    vi.mocked(startTenantSwitch).mockRejectedValue(new Error('discovery failed'));
    const user = userEvent.setup();
    renderWithSession();

    await user.click(
      await screen.findByRole('button', { name: 'Example Platform, switch tenant' }),
    );
    await user.click(await screen.findByRole('button', { name: 'Example Data' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not start the switch.');
    expect(screen.getByRole('button', { name: 'Example Data' })).toBeEnabled();
  });

  it('re-enables the list when the page returns from the back-forward cache', async () => {
    vi.mocked(startTenantSwitch).mockReturnValue(new Promise(() => {}));
    const user = userEvent.setup();
    renderWithSession();

    await user.click(
      await screen.findByRole('button', { name: 'Example Platform, switch tenant' }),
    );
    await user.click(await screen.findByRole('button', { name: 'Example Data' }));
    expect(screen.getByRole('button', { name: 'Example Data' })).toBeDisabled();

    const restored = new Event('pageshow');
    Object.defineProperty(restored, 'persisted', { value: true });
    act(() => {
      window.dispatchEvent(restored);
    });

    expect(screen.getByRole('button', { name: 'Example Data' })).toBeEnabled();
  });
});
