/** Unit tests for the skill files: SKILL.md first, and a picked file in the preview. */
import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@runkora/test-utils';
import { STORY_SKILL } from '../../../_components/skills-story-fixtures';
import { SkillDetail } from './skill-detail';

describe('SkillDetail', () => {
  it('previews SKILL.md first, then the file picked in the explorer', async () => {
    renderWithProviders(<SkillDetail skill={STORY_SKILL} />);

    expect(screen.getByRole('figure')).toHaveTextContent('Read the PDF');
    await userEvent.click(screen.getByRole('button', { name: /reference/ }));
    await userEvent.click(screen.getByRole('button', { name: /forms\.md/ }));

    expect(screen.getByRole('figure')).toHaveTextContent('Fill each field in order.');
  });
});
