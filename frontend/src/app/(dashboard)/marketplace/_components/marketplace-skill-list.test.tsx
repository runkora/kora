import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { MarketplaceProvider } from '../_context/marketplace-context';
import { MarketplaceSkillList } from './marketplace-skill-list';
import type { MarketplaceOverviewData } from '../_types';
import { buildBrowseUrl } from '../_utils/browse-params';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

const BROWSE = { marketplace: 'Core', query: '', page: 1, pageSize: 24, sort: 'name-asc' as const };

function renderList(overview: MarketplaceOverviewData) {
  window.history.replaceState(null, '', buildBrowseUrl('/marketplace', '', overview.browse));
  return renderWithProviders(
    <MarketplaceProvider initialData={overview} initialError={null}>
      <MarketplaceSkillList />
    </MarketplaceProvider>,
  );
}

describe('MarketplaceSkillList rendering', () => {
  it('renders every Skill with its title, slug, and description', () => {
    renderList({
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

  it('renders without crashing when a description is empty', () => {
    renderList({
      marketplaces: [{ name: 'Core' }],
      requested: BROWSE,
      browse: BROWSE,
      skills: [{ slug: 'core/skills/blank', title_of_package: 'Blank', description: '' }],
      total: 1,
    });

    expect(screen.getByText('Blank')).toBeInTheDocument();
  });

  it('renders a long description in full (truncation is a CSS concern, not a content one)', () => {
    const longDescription = 'A very long description. '.repeat(20).trim();
    renderList({
      marketplaces: [{ name: 'Core' }],
      requested: BROWSE,
      browse: BROWSE,
      skills: [
        { slug: 'core/skills/long', title_of_package: 'Long', description: longDescription },
      ],
      total: 1,
    });

    expect(screen.getByText(longDescription)).toBeInTheDocument();
  });

  it('shows the current Marketplace name for every row', () => {
    renderList({
      marketplaces: [{ name: 'Core' }],
      requested: BROWSE,
      browse: BROWSE,
      skills: [
        { slug: 'core/skills/a', title_of_package: 'A', description: '' },
        { slug: 'core/skills/b', title_of_package: 'B', description: '' },
      ],
      total: 2,
    });

    expect(screen.getAllByText('Core')).toHaveLength(2);
  });
});
