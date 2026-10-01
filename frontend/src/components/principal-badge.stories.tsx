import type { Meta, StoryObj } from '@storybook/react';
import { PrincipalBadge, type PrincipalType } from './principal-badge';

const meta: Meta<typeof PrincipalBadge> = {
  title: 'App/Shared/PrincipalBadge',
  component: PrincipalBadge,
  tags: ['autodocs'],
  argTypes: {
    type: {
      control: 'select',
      options: ['AGENT', 'HUMAN', 'PAT', 'SERVICE'] satisfies PrincipalType[],
    },
  },
};
export default meta;
type Story = StoryObj<typeof PrincipalBadge>;

export const Agent: Story = { args: { type: 'AGENT' } };
export const Human: Story = { args: { type: 'HUMAN' } };
export const Pat: Story = { args: { type: 'PAT' } };
export const Service: Story = { args: { type: 'SERVICE' } };
