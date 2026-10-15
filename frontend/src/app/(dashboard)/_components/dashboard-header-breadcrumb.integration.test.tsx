/**
 * Integration tests for the header breadcrumb of DashboardHeader in the
 * Marketplace.
 *
 * On a Skill page, the header shows "Marketplace › {last part of the path}".
 * The header has no Back button (#340). The Skill page has its own back link
 * to the same overview results.
 *
 * `next/navigation` is mocked, so each test controls the path.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { APP_ROUTES } from '@orbit/lib/routing';
import { SidebarStateProvider } from './sidebar-state';
import { DashboardHeader } from './dashboard-header';

vi.mock('next/navigation', () => ({
  usePathname: vi.fn(),
}));

import { usePathname } from 'next/navigation';

const SKILL_PATH = APP_ROUTES.marketplace.detail('platform-engineering/skills/react-reviewer');

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
  it('Skill page: shows "Marketplace" and the last part of the path', () => {
    renderHeader(SKILL_PATH);

    expect(screen.getByText('Marketplace')).toBeInTheDocument();
    expect(screen.getByText('react-reviewer')).toBeInTheDocument();
  });

  it('Skill page: has no Back link', () => {
    renderHeader(SKILL_PATH);

    expect(screen.queryByRole('link', { name: 'Back' })).not.toBeInTheDocument();
  });

  it('Skill page: decodes reserved characters in the breadcrumb label', () => {
    renderHeader(APP_ROUTES.marketplace.detail('core/skills/reviewer?#%'));

    expect(screen.getByText('reviewer?#%')).toBeInTheDocument();
  });

  it('overview: shows "Marketplace" as the section title', () => {
    renderHeader('/marketplace');

    expect(screen.getByText('Marketplace')).toBeInTheDocument();
  });
});
