import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { SkillChips } from './skill-chips';

const skill = { manualOnly: false, tags: [], tools: [] };

describe('SkillChips', () => {
  it('labels a skill with the folder above it', () => {
    render(<SkillChips skill={{ ...skill, path: 'skills/engineering/tdd' }} />);
    expect(screen.getByText('Engineering')).toBeInTheDocument();
  });

  it('shows no folder chip for the plugin layout', () => {
    const { container } = render(
      <SkillChips skill={{ ...skill, path: 'plugins/example-plugin/skills/tdd' }} />,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
