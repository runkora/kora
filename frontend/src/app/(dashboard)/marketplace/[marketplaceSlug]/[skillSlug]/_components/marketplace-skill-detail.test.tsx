import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MarketplaceSkillDetail } from './marketplace-skill-detail';
import type { Skill } from '@orbit/shared/api';

const resources: Skill['documents'] = [
  {
    path: 'skills/react-expert/notes.md',
    name: 'notes.md',
    content: '# Hook notes\n\nKeep **hooks** pure.\n[Hook guide](https://example.test/hooks)',
  },
  {
    path: 'skills/react-expert/examples.html',
    name: 'examples.html',
    content:
      '<h2>Resource heading</h2>\n<a href="https://example.test/resources">Resource link</a>\n<strong>Resource emphasis</strong>',
  },
];

const MARKETPLACE_NAME = 'Core';

function makeSkill(overrides: Partial<Skill> = {}): Skill {
  return {
    skillSlug: 'core__skills__react-expert',
    name: 'React Expert',
    description: 'Senior React developer.',
    skill: {
      path: 'skills/react-expert/SKILL.md',
      name: 'SKILL.md',
      content: '# React Expert\n\nSenior React developer.',
    },
    documents: [],
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
    render(<MarketplaceSkillDetail skill={makeSkill()} marketplaceName={MARKETPLACE_NAME} />);
    expect(screen.getByRole('heading', { name: 'React Expert' })).toBeInTheDocument();
    expect(screen.getAllByText('Core').length).toBeGreaterThan(0);
    expect(screen.getByText('Senior React developer.')).toBeInTheDocument();
  });

  it.each([
    { count: '0 resources', files: [] },
    { count: '1 resource', files: resources.slice(0, 1) },
    { count: '2 resources', files: resources },
  ])('renders SKILL.md and $count with their matching contents', ({ count, files }) => {
    const skill = makeSkill({ documents: files });
    render(<MarketplaceSkillDetail skill={skill} marketplaceName={MARKETPLACE_NAME} />);

    expect(screen.getByText(count)).toBeInTheDocument();
    expect(screen.getAllByRole('figure')).toHaveLength(files.length + 1);
    expect(fileBlock(skill.skill.name).querySelector('code')?.textContent).toBe(
      skill.skill.content,
    );
    for (const resource of files) {
      expect(fileBlock(resource.name).querySelector('code')?.textContent).toBe(resource.content);
    }
  });

  it('keeps Markdown and HTML-looking instructions and resources as literal text', () => {
    const skill = makeSkill({ documents: resources });
    skill.skill.content = [
      '# Instruction heading',
      '',
      '**Instruction emphasis** and [Instruction guide](https://example.test/instructions)',
      '<h2>HTML instruction heading</h2>',
      '<a href="https://example.test/instructions">HTML instruction link</a>',
      '<strong>HTML instruction emphasis</strong>',
    ].join('\n');
    render(<MarketplaceSkillDetail skill={skill} marketplaceName={MARKETPLACE_NAME} />);

    expect(fileBlock(skill.skill.name).querySelector('code')?.textContent).toBe(
      skill.skill.content,
    );
    for (const block of screen.getAllByRole('figure')) {
      expect(within(block).queryAllByRole('heading')).toHaveLength(0);
      expect(within(block).queryAllByRole('link')).toHaveLength(0);
      expect(block.querySelector('strong, em')).toBeNull();
    }
  });

  it('shows the Marketplace and path in the Source panel', () => {
    const skill = makeSkill();
    render(<MarketplaceSkillDetail skill={skill} marketplaceName={MARKETPLACE_NAME} />);

    // Card has no landmark; scope to Source's nearest container with its metadata label.
    let sourcePanel = screen.getByRole('heading', { name: 'Source' }).parentElement;
    while (sourcePanel && !within(sourcePanel).queryByText('Marketplace')) {
      sourcePanel = sourcePanel.parentElement;
    }
    if (!sourcePanel) throw new Error('Missing Source panel');
    const source = within(sourcePanel);

    expect(source.getByText('Marketplace')).toBeInTheDocument();
    expect(source.getByText(MARKETPLACE_NAME)).toBeInTheDocument();
    expect(source.getByText('Path')).toBeInTheDocument();
    expect(source.getByText(skill.skill.path)).toBeInTheDocument();
  });
});
