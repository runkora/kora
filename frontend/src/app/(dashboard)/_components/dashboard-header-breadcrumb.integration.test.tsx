/**
 * Integration tests for the header of DashboardHeader in Skills.
 *
 * The overview shows the section title; a skill page shows the section and the
 * skill's folder name, following the Agents detail pattern.
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

describe('DashboardHeader in Skills', () => {
  it('overview: shows "Skills" as the section title', () => {
    renderHeader('/skills');

    expect(screen.getByText('Skills')).toBeInTheDocument();
  });

  it('overview: has no Back link', () => {
    renderHeader('/skills');

    expect(screen.queryByRole('link', { name: 'Back' })).not.toBeInTheDocument();
  });

  it("skill page: shows the skill's folder name as the breadcrumb label", () => {
    renderHeader(APP_ROUTES.skills.skill('team-skills', 'skills/react-expert'));

    expect(screen.getByText('Skills')).toBeInTheDocument();
    expect(screen.getByText('react-expert')).toBeInTheDocument();
    expect(screen.queryByText('team-skills')).not.toBeInTheDocument();
  });
});
