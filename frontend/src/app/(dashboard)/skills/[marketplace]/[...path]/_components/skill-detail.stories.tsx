/** @tier 3 — skills/[marketplace]/[...path]-scoped. One skill's files and its source repository. */

import type { Meta, StoryObj } from '@storybook/react';
import { STORY_SKILL } from '../../../_components/skills-story-fixtures';
import { SkillDetail } from './skill-detail';

const meta: Meta<typeof SkillDetail> = {
  title: 'Pages/Skills/SkillDetail',
  component: SkillDetail,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof SkillDetail>;

export const Default: Story = { args: { skill: STORY_SKILL, marketplace: 'team-skills' } };

export const OnlySkillMd: Story = {
  args: { skill: { ...STORY_SKILL, files: [] }, marketplace: 'team-skills' },
};
