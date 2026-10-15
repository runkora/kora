/**
 * Integration tests for the header of DashboardHeader in the Marketplace.
 *
 * A Skill opens in place on the overview, so the path is always
 * `/marketplace`. The header shows the section title without a breadcrumb.
 *
 * `next/navigation` is mocked, so each test controls the path.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { SidebarStateProvider } from './sidebar-state';
import { DashboardHeader } from './dashboard-header';

vi.mock('next/navigation', () => ({
  usePathname: vi.fn(),
}));

import { usePathname } from 'next/navigation';

function renderHeader(pathname: string) {
  vi.mocked(usePathname).mockReturnValue(pathname);
  return renderWithProviders(
    <SidebarStateProvider>
      <DashboardHeader />
    </SidebarStateProvider>,
  );
}

afterEach(() => {
  vi.mocked(usePathname).mockReset();
});

describe('DashboardHeader in the Marketplace', () => {
  it('overview: shows "Marketplace" as the section title', () => {
    renderHeader('/marketplace');

    expect(screen.getByText('Marketplace')).toBeInTheDocument();
  });

  it('overview: has no Back link', () => {
    renderHeader('/marketplace');

    expect(screen.queryByRole('link', { name: 'Back' })).not.toBeInTheDocument();
  });
});
