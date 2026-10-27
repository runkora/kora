import { describe, it, expect } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { MarketplaceSkillDetail } from './marketplace-skill-detail';
import { repositoryName } from './skill-repository-card';
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
  {
    path: 'skills/react-expert/references/guide.md',
    name: 'guide.md',
    content: '# Guide',
  },
];

const REPO = 'https://github.com/anthropics/skills';

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

/** The preview block, found by the filename in its header (the explorer lists the same name). */
function fileBlock(filename: string) {
  const block = screen
    .getAllByRole('figure')
    .find((figure) => within(figure).queryByText(filename) !== null);
  if (!block) throw new Error(`Missing content block for ${filename}`);
  return block;
}

describe('MarketplaceSkillDetail', () => {
  it('leaves out the title, description, and Source panel', () => {
    render(<MarketplaceSkillDetail skill={makeSkill()} repositoryUrl={REPO} />);
    expect(screen.queryByRole('heading', { name: 'React Expert' })).not.toBeInTheDocument();
    expect(screen.queryByText('Senior React developer.')).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Source' })).not.toBeInTheDocument();
  });

  it.each([
    { count: '1 file', files: [] },
    { count: '2 files', files: resources.slice(0, 1) },
    { count: '4 files', files: resources },
  ])(
    'counts SKILL.md and its resources as $count and previews SKILL.md first',
    ({ count, files }) => {
      const skill = makeSkill({ documents: files });
      render(<MarketplaceSkillDetail skill={skill} repositoryUrl={REPO} />);

      expect(screen.getByText(count)).toBeInTheDocument();
      expect(screen.getAllByRole('figure')).toHaveLength(1);
      expect(screen.getByRole('button', { name: /SKILL\.md/ })).toHaveAttribute(
        'aria-current',
        'true',
      );
      expect(fileBlock(skill.skill.name).querySelector('code')?.textContent).toBe(
        skill.skill.content,
      );
    },
  );

  it('lists folders before files and shows sizes', () => {
    render(
      <MarketplaceSkillDetail skill={makeSkill({ documents: resources })} repositoryUrl={REPO} />,
    );

    const rows = screen
      .getAllByRole('button')
      .map((b) => b.textContent ?? '')
      .filter((t) => /references|SKILL|notes|examples/.test(t));
    expect(rows[0]).toContain('references');
    expect(rows.slice(1).map((t) => t.replace(/\d.*$/, ''))).toEqual([
      'examples.html',
      'notes.md',
      'SKILL.md',
    ]);
    expect(screen.getByRole('button', { name: /SKILL\.md/ })).toHaveTextContent(/\d+ B$/);
  });

  it('previews the file that is clicked, including files inside a folder', () => {
    render(
      <MarketplaceSkillDetail skill={makeSkill({ documents: resources })} repositoryUrl={REPO} />,
    );

    fireEvent.click(screen.getByRole('button', { name: /notes\.md/ }));
    expect(fileBlock('notes.md').querySelector('code')?.textContent).toBe(resources[0].content);
    expect(screen.getAllByRole('figure')).toHaveLength(1);

    expect(screen.queryByRole('button', { name: /guide\.md/ })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /references/ }));
    fireEvent.click(screen.getByRole('button', { name: /guide\.md/ }));
    expect(fileBlock('references/guide.md').querySelector('code')?.textContent).toBe('# Guide');
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
    render(<MarketplaceSkillDetail skill={skill} repositoryUrl={REPO} />);

    expect(fileBlock(skill.skill.name).querySelector('code')?.textContent).toBe(
      skill.skill.content,
    );
    for (const name of ['SKILL.md', 'notes.md', 'examples.html']) {
      fireEvent.click(screen.getByRole('button', { name: new RegExp(name.replace('.', '\\.')) }));
      const block = fileBlock(name);
      expect(within(block).queryAllByRole('heading')).toHaveLength(0);
      expect(within(block).queryAllByRole('link')).toHaveLength(0);
      expect(block.querySelector('strong, em')).toBeNull();
    }
  });

  it('shows the source repository, with no link to GitHub', () => {
    render(<MarketplaceSkillDetail skill={makeSkill()} repositoryUrl={REPO} />);

    expect(screen.getByText('anthropics')).toBeInTheDocument();
    expect(screen.getByText('anthropics/skills')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /GitHub/ })).not.toBeInTheDocument();
  });

  it('collapses and expands the file list from the bar under it', () => {
    render(
      <MarketplaceSkillDetail skill={makeSkill({ documents: resources })} repositoryUrl={REPO} />,
    );

    expect(screen.getByRole('button', { name: /notes\.md/ })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Hide files' }));
    expect(screen.queryByRole('button', { name: /notes\.md/ })).not.toBeInTheDocument();
    // The preview stays, so the selected file is still shown.
    expect(screen.getAllByRole('figure')).toHaveLength(1);
    fireEvent.click(screen.getByRole('button', { name: 'Show files' }));
    expect(screen.getByRole('button', { name: /notes\.md/ })).toBeInTheDocument();
  });
});

describe('repositoryName', () => {
  it('reads the owner and owner/repo from a GitHub link', () => {
    expect(repositoryName('https://github.com/anthropics/skills')).toEqual({
      owner: 'anthropics',
      fullName: 'anthropics/skills',
    });
    expect(repositoryName('https://github.com/a/b.git/')).toEqual({ owner: 'a', fullName: 'a/b' });
  });

  it('falls back to the raw link when it has no owner and repo', () => {
    expect(repositoryName('not a url')).toEqual({ owner: 'not a url', fullName: 'not a url' });
  });
});
