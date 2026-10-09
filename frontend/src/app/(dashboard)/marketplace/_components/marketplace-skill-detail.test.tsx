import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MarketplaceSkillDetail } from './marketplace-skill-detail';
import type { PackageDetail } from '@orbit/shared/api';

const resources: PackageDetail['resources_of_package'] = [
  {
    path_of_resource_in_repo: 'skills/react-expert/notes.md',
    name_of_resource: 'notes.md',
    body_of_resource:
      '# Hook notes\n\nKeep **hooks** pure.\n[Hook guide](https://example.test/hooks)',
  },
  {
    path_of_resource_in_repo: 'skills/react-expert/examples.html',
    name_of_resource: 'examples.html',
    body_of_resource:
      '<h2>Resource heading</h2>\n<a href="https://example.test/resources">Resource link</a>\n<strong>Resource emphasis</strong>',
  },
];

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

function fileBlock(filename: string) {
  const block = screen.getByText(filename).closest('figure');
  if (!block) throw new Error(`Missing content block for ${filename}`);
  return block;
}

describe('MarketplaceSkillDetail', () => {
  it('renders the title, Marketplace, and description', () => {
    render(<MarketplaceSkillDetail skill={makeSkill()} />);
    expect(screen.getByRole('heading', { name: 'React Expert' })).toBeInTheDocument();
    expect(screen.getAllByText('Core').length).toBeGreaterThan(0);
    expect(screen.getByText('Senior React developer.')).toBeInTheDocument();
  });

  it.each([
    { count: '0 resources', files: [] },
    { count: '1 resource', files: resources.slice(0, 1) },
    { count: '2 resources', files: resources },
  ])('renders SKILL.md and $count with their matching contents', ({ count, files }) => {
    const skill = makeSkill({ resources_of_package: files });
    render(<MarketplaceSkillDetail skill={skill} />);

    expect(screen.getByText(count)).toBeInTheDocument();
    expect(screen.getAllByRole('figure')).toHaveLength(files.length + 1);
    expect(fileBlock(skill.package.name_of_package).querySelector('code')?.textContent).toBe(
      skill.package.body_of_package,
    );
    for (const resource of files) {
      expect(fileBlock(resource.name_of_resource).querySelector('code')?.textContent).toBe(
        resource.body_of_resource,
      );
    }
  });

  it('keeps Markdown and HTML-looking instructions and resources as literal text', () => {
    const skill = makeSkill({ resources_of_package: resources });
    skill.package.body_of_package = [
      '# Instruction heading',
      '',
      '**Instruction emphasis** and [Instruction guide](https://example.test/instructions)',
      '<h2>HTML instruction heading</h2>',
      '<a href="https://example.test/instructions">HTML instruction link</a>',
      '<strong>HTML instruction emphasis</strong>',
    ].join('\n');
    render(<MarketplaceSkillDetail skill={skill} />);

    expect(fileBlock(skill.package.name_of_package).querySelector('code')?.textContent).toBe(
      skill.package.body_of_package,
    );
    for (const block of screen.getAllByRole('figure')) {
      expect(within(block).queryAllByRole('heading')).toHaveLength(0);
      expect(within(block).queryAllByRole('link')).toHaveLength(0);
      expect(block.querySelector('strong, em')).toBeNull();
    }
  });

  it('shows the Marketplace and path in the Source panel', () => {
    const skill = makeSkill();
    render(<MarketplaceSkillDetail skill={skill} />);

    // Card has no landmark; scope to Source's nearest container with its metadata label.
    let sourcePanel = screen.getByRole('heading', { name: 'Source' }).parentElement;
    while (sourcePanel && !within(sourcePanel).queryByText('Marketplace')) {
      sourcePanel = sourcePanel.parentElement;
    }
    if (!sourcePanel) throw new Error('Missing Source panel');
    const source = within(sourcePanel);

    expect(source.getByText('Marketplace')).toBeInTheDocument();
    expect(source.getByText(skill.name_of_marketplace)).toBeInTheDocument();
    expect(source.getByText('Path')).toBeInTheDocument();
    expect(source.getByText(skill.package.path_of_package_in_repo)).toBeInTheDocument();
  });
});
