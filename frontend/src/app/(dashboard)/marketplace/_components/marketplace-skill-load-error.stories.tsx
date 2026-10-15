/** @tier 3 — marketplace-scoped. Preview of the Skill load failure and retry action. */

import type { Meta, StoryObj } from '@storybook/react';
import { fn } from 'storybook/test';
import { MarketplaceSkillLoadError } from './marketplace-skill-load-error';

const meta: Meta<typeof MarketplaceSkillLoadError> = {
  title: 'Pages/Marketplace/SkillLoadError',
  component: MarketplaceSkillLoadError,
  tags: ['autodocs'],
  parameters: {
    layout: 'padded',
  },
  args: {
    onRetry: fn(),
  },
};
export default meta;
type Story = StoryObj<typeof MarketplaceSkillLoadError>;

export const Default: Story = {};
