import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { MarketplaceProvider } from '../_context/marketplace-context';
import { MarketplaceSkillGrid } from './marketplace-skill-grid';
import type { MarketplaceOverviewData } from '../_types';
import { buildBrowseUrl } from '../_utils/browse-params';

const BROWSE = { marketplace: 'Core', query: '', page: 1, pageSize: 24, sort: 'name-asc' as const };

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

function renderGrid(overview: MarketplaceOverviewData) {
  window.history.replaceState(null, '', buildBrowseUrl('/marketplace', '', overview.browse));
  return renderWithProviders(
    <MarketplaceProvider initialData={overview} initialError={null}>
      <MarketplaceSkillGrid />
    </MarketplaceProvider>,
  );
}

describe('MarketplaceSkillGrid rendering', () => {
  it('renders every Skill as its own card with title, slug, and description', () => {
    renderGrid({
      marketplaces: [{ name: 'Core' }],
      requested: BROWSE,
      browse: BROWSE,
      skills: [
        {
          slug: 'core/skills/react-expert',
          title_of_package: 'React Expert',
          description: 'Senior React developer.',
        },
        {
          slug: 'core/skills/go-expert',
          title_of_package: 'Go Expert',
          description: 'Senior Go developer.',
        },
      ],
      total: 2,
    });

    expect(screen.getByText('React Expert')).toBeInTheDocument();
    expect(screen.getByText('core/skills/react-expert')).toBeInTheDocument();
    expect(screen.getByText('Senior React developer.')).toBeInTheDocument();
    expect(screen.getByText('Go Expert')).toBeInTheDocument();
  });

  it('shows a placeholder for an empty description', () => {
    renderGrid({
      marketplaces: [{ name: 'Core' }],
      requested: BROWSE,
      browse: BROWSE,
      skills: [{ slug: 'core/skills/blank', title_of_package: 'Blank', description: '' }],
      total: 1,
    });

    expect(screen.getByText('No description provided.')).toBeInTheDocument();
  });

  it('renders the same result set that the list view renders', () => {
    const skills = [
      { slug: 'core/skills/a', title_of_package: 'A', description: 'Skill A' },
      { slug: 'core/skills/b', title_of_package: 'B', description: 'Skill B' },
    ];
    renderGrid({
      marketplaces: [{ name: 'Core' }],
      requested: BROWSE,
      browse: BROWSE,
      skills,
      total: 2,
    });

    expect(screen.getAllByRole('link')).toHaveLength(2);
  });
});
