/**
 * CodeBlock may apply syntax highlighting that splits text into multiple
 * nested elements, so file-content assertions check document.body.textContent
 * rather than a single getByText match.
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MarketplaceSkillDetail } from './marketplace-skill-detail';
import type { PackageDetail } from '@orbit/shared/api';

function makeSkill(overrides: Partial<PackageDetail> = {}): PackageDetail {
  return {
    slug: 'core/skills/react-expert',
    title_of_package: 'React Expert',
    description: 'Senior React developer.',
    name_of_marketplace: 'Core',
    package: {
      path_of_package_in_repo: 'skills/react-expert/SKILL.md',
      name_of_package: 'SKILL.md',
      body_of_package: '# React Expert\n\nSenior React developer.',
    },
    resources_of_package: [],
    ...overrides,
  };
}

describe('MarketplaceSkillDetail', () => {
  it('renders the title, Marketplace, and description', () => {
    render(<MarketplaceSkillDetail skill={makeSkill()} />);
    expect(screen.getByRole('heading', { name: 'React Expert' })).toBeInTheDocument();
    expect(screen.getAllByText('Core').length).toBeGreaterThan(0);
    expect(screen.getByText('Senior React developer.')).toBeInTheDocument();
  });

  it('shows a singular resource count for exactly one resource', () => {
    render(
      <MarketplaceSkillDetail
        skill={makeSkill({
          resources_of_package: [
            {
              path_of_resource_in_repo: 'skills/react-expert/notes.md',
              name_of_resource: 'notes.md',
              body_of_resource: 'Notes.',
            },
          ],
        })}
      />,
    );
    expect(screen.getByText('1 resource')).toBeInTheDocument();
  });

  it('shows a plural resource count for many resources', () => {
    render(
      <MarketplaceSkillDetail
        skill={makeSkill({
          resources_of_package: [
            { path_of_resource_in_repo: 'a.md', name_of_resource: 'a.md', body_of_resource: 'A' },
            { path_of_resource_in_repo: 'b.md', name_of_resource: 'b.md', body_of_resource: 'B' },
          ],
        })}
      />,
    );
    expect(screen.getByText('2 resources')).toBeInTheDocument();
  });

  it('shows zero resources when the Skill has none', () => {
    render(<MarketplaceSkillDetail skill={makeSkill({ resources_of_package: [] })} />);
    expect(screen.getByText('0 resources')).toBeInTheDocument();
  });

  it('renders the package file and each resource file as plain text', () => {
    render(
      <MarketplaceSkillDetail
        skill={makeSkill({
          resources_of_package: [
            {
              path_of_resource_in_repo: 'skills/react-expert/notes.md',
              name_of_resource: 'notes.md',
              body_of_resource: 'Keep hooks pure.',
            },
          ],
        })}
      />,
    );
    expect(document.body.textContent).toContain('# React Expert');
    expect(document.body.textContent).toContain('Keep hooks pure.');
  });

  it('shows the Marketplace and path in the Source panel', () => {
    render(<MarketplaceSkillDetail skill={makeSkill()} />);
    expect(screen.getByText('Path')).toBeInTheDocument();
    expect(document.body.textContent).toContain('skills/react-expert/SKILL.md');
  });
});
