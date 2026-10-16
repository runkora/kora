/**
 * Integration tests for the header of DashboardHeader in the Marketplace.
 *
 * The overview shows the section title; the dedicated Skill path shows the
 * section and requested Skill identifier, following the Agents detail pattern.
 *
 * `next/navigation` is mocked, so each test controls the path.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { SidebarStateProvider } from './sidebar-state';
import { DashboardHeader } from './dashboard-header';
import { APP_ROUTES } from '@orbit/lib/routing';

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

  it('detail: shows the Skill identifier as the breadcrumb label', () => {
    const skillSlug = 'platform-engineering__skills__react-expert';
    renderHeader(APP_ROUTES.marketplace.detail('platform-engineering__skills', skillSlug));

    expect(screen.getByText('Marketplace')).toBeInTheDocument();
    expect(screen.getByText(skillSlug)).toBeInTheDocument();
    expect(screen.queryByText('platform-engineering__skills')).not.toBeInTheDocument();
  });
});
