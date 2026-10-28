import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { MarketplaceProvider } from '../_context/marketplace-context';
import { MarketplaceSkillList } from './marketplace-skill-list';
import type { MarketplaceOverviewData } from '../_types';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

const BROWSE = { marketplace: 'Core', query: '', page: 1, pageSize: 24, sort: 'name-asc' as const };

function renderList(overview: MarketplaceOverviewData) {
  window.history.replaceState(null, '', '/marketplace');
  return renderWithProviders(
    <MarketplaceProvider initialData={overview} initialError={null}>
      <MarketplaceSkillList />
    </MarketplaceProvider>,
  );
}

describe('MarketplaceSkillList rendering', () => {
  it('renders every Skill with its title, slug, and description', () => {
    renderList({
      marketplaces: [
        { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
      ],
      requested: BROWSE,
      browse: BROWSE,
      skills: [
        {
          skillSlug: 'core/skills/react-expert',
          name: 'React Expert',
          description: 'Senior React developer.',
        },
        {
          skillSlug: 'core/skills/go-expert',
          name: 'Go Expert',
          description: 'Senior Go developer.',
        },
      ],
      total: 2,
    });

    expect(screen.getByText('React Expert')).toBeInTheDocument();
    expect(screen.queryByText('core/skills/react-expert')).not.toBeInTheDocument();
    expect(screen.getByText('Senior React developer.')).toBeInTheDocument();
    expect(screen.getByText('Go Expert')).toBeInTheDocument();
  });

  it('renders without crashing when a description is empty', () => {
    renderList({
      marketplaces: [
        { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
      ],
      requested: BROWSE,
      browse: BROWSE,
      skills: [{ skillSlug: 'core/skills/blank', name: 'Blank', description: '' }],
      total: 1,
    });

    expect(screen.getByText('Blank')).toBeInTheDocument();
  });

  it('renders a long description in full (truncation is a CSS concern, not a content one)', () => {
    const longDescription = 'A very long description. '.repeat(20).trim();
    renderList({
      marketplaces: [
        { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
      ],
      requested: BROWSE,
      browse: BROWSE,
      skills: [{ skillSlug: 'core/skills/long', name: 'Long', description: longDescription }],
      total: 1,
    });

    expect(screen.getByText(longDescription)).toBeInTheDocument();
  });

  it('shows the current Marketplace name for every row', () => {
    renderList({
      marketplaces: [
        { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
      ],
      requested: BROWSE,
      browse: BROWSE,
      skills: [
        { skillSlug: 'core/skills/a', name: 'A', description: '' },
        { skillSlug: 'core/skills/b', name: 'B', description: '' },
      ],
      total: 2,
    });

    expect(screen.getAllByText('Core')).toHaveLength(2);
  });
});
