/**
 * Integration tests for MobileSidebarDrawer.
 *
 * Strategy:
 * - `next/navigation` and `./permissions-provider` are mocked so the embedded
 *   DashboardSidebar / SidebarUserFooter render without a live session or MSW.
 * - A small harness exposes an open trigger via `useSidebarState`, since the
 *   drawer is driven entirely by the shared sidebar context. Radix renders the
 *   drawer into a portal, so queries run against the whole document.
 */
import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Button } from '@orbit/terra';
import { renderWithProviders } from '@orbit/test-utils';
import { SidebarStateProvider, useSidebarState } from './sidebar-state';
import { MobileSidebarDrawer } from './mobile-sidebar-drawer';

vi.mock('next/navigation', () => ({
  usePathname: vi.fn().mockReturnValue('/'),
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock('./permissions-provider', () => ({
  useSession: vi.fn().mockReturnValue({
    scopes: [],
    user: { sub: 'user-test-123', email: 'dev@orbit.local', name: 'Dev User' },
    loaded: true,
  }),
}));

function Harness() {
  const { open } = useSidebarState();
  return (
    <>
      <Button onClick={open}>open-drawer</Button>
      <MobileSidebarDrawer />
    </>
  );
}

function renderDrawer() {
  return renderWithProviders(
    <SidebarStateProvider>
      <Harness />
    </SidebarStateProvider>,
  );
}

async function openDrawer(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'open-drawer' }));
}

describe('MobileSidebarDrawer', () => {
  it('renders no drawer content while closed', () => {
    renderDrawer();
    expect(screen.queryByRole('button', { name: 'Close navigation' })).not.toBeInTheDocument();
    expect(screen.queryByText('Control Plane')).not.toBeInTheDocument();
  });

  it('renders the sidebar nav, brand, and footer when opened', async () => {
    const user = userEvent.setup();
    renderDrawer();
    await openDrawer(user);

    // Nav groups from the reused DashboardSidebar
    expect(screen.getByText('Control Plane')).toBeInTheDocument();
    // Reused SidebarBrandHeader (single source of brand + version)
    expect(screen.getByText('kora')).toBeInTheDocument();
    expect(screen.getByText('dev')).toBeInTheDocument();
    // Reused SidebarUserFooter
    expect(screen.getByText('Dev User')).toBeInTheDocument();
    expect(screen.getByText('dev@orbit.local')).toBeInTheDocument();
    // Accessible dialog title
    expect(screen.getByText('Navigation')).toBeInTheDocument();
  });

  it('closes when the close button is clicked', async () => {
    const user = userEvent.setup();
    renderDrawer();
    await openDrawer(user);

    await user.click(screen.getByRole('button', { name: 'Close navigation' }));
    expect(screen.queryByText('Control Plane')).not.toBeInTheDocument();
  });

  it('closes when a nav link is clicked (onNavigate)', async () => {
    const user = userEvent.setup();
    renderDrawer();
    await openDrawer(user);

    await user.click(screen.getByRole('link', { name: /dashboard/i }));
    expect(screen.queryByText('Control Plane')).not.toBeInTheDocument();
  });
});
