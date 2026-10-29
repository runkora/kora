import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { MarketplaceProvider } from '../_context/marketplace-context';
import { MarketplaceSkillGrid } from './marketplace-skill-grid';
import type { MarketplaceOverviewData } from '../_types';

const BROWSE = { marketplace: 'Core', query: '', page: 1, pageSize: 24, sort: 'name-asc' as const };

function renderGrid(overview: MarketplaceOverviewData) {
  window.history.replaceState(null, '', '/marketplace');
  return renderWithProviders(
    <MarketplaceProvider initialData={overview} initialError={null}>
      <MarketplaceSkillGrid />
    </MarketplaceProvider>,
  );
}

describe('MarketplaceSkillGrid rendering', () => {
  it('renders every Skill as its own card with title, slug, and description', () => {
    renderGrid({
      marketplaces: [
        { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
      ],
      requested: BROWSE,
      browse: BROWSE,
      skills: [
        {
          skillSlug: 'core/skills/react-expert',
          skillName: 'react-expert',
          name: 'React Expert',
          description: 'Senior React developer.',
        },
        {
          skillSlug: 'core/skills/go-expert',
          skillName: 'go-expert',
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

  it('shows a placeholder for an empty description', () => {
    renderGrid({
      marketplaces: [
        { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
      ],
      requested: BROWSE,
      browse: BROWSE,
      skills: [
        { skillSlug: 'core/skills/blank', skillName: 'blank', name: 'Blank', description: '' },
      ],
      total: 1,
    });

    expect(screen.getByText('No description provided.')).toBeInTheDocument();
  });

  it('renders the same result set that the list view renders', () => {
    const skills = [
      { skillSlug: 'core/skills/a', skillName: 'a', name: 'A', description: 'Skill A' },
      { skillSlug: 'core/skills/b', skillName: 'b', name: 'B', description: 'Skill B' },
    ];
    renderGrid({
      marketplaces: [
        { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
      ],
      requested: BROWSE,
      browse: BROWSE,
      skills,
      total: 2,
    });

    expect(screen.getAllByRole('link', { name: /^Open / })).toHaveLength(2);
  });
});
