/** @tier 3 — skills/[marketplace]/[...path]-scoped. A skill that could not load, with a retry. */

import type { Meta, StoryObj } from '@storybook/react';
import { SkillLoadError } from './skill-load-error';

const meta: Meta<typeof SkillLoadError> = {
  title: 'Pages/Skills/SkillLoadError',
  component: SkillLoadError,
  tags: ['autodocs'],
  parameters: { layout: 'padded', nextjs: { appDirectory: true } },
};
export default meta;
type Story = StoryObj<typeof SkillLoadError>;

export const UnavailableMarketplace: Story = {
  args: { status: 502, message: 'This Marketplace could not be loaded. Try again later.' },
};

export const LoadFailed: Story = {
  args: { status: 500, message: 'Could not load this skill. Try again.' },
};
