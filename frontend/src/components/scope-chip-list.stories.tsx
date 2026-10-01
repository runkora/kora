import type { Meta, StoryObj } from '@storybook/react';
import { ScopeChipList } from './scope-chip-list';

const meta: Meta<typeof ScopeChipList> = {
  title: 'App/Shared/ScopeChipList',
  component: ScopeChipList,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof ScopeChipList>;

export const Few: Story = {
  args: { scopes: ['inference:invoke', 'agents:runs:read'] },
};

export const OverflowsIntoPopover: Story = {
  args: {
    scopes: [
      'inference:invoke',
      'inference:anthropic:orbit-claude-sonnet:use',
      'connection:github:metadata:read',
      'connection:github:contents:read',
      'connection:github:contents:write',
      'egress:github.com:connect',
    ],
  },
};

export const CustomMax: Story = {
  args: { scopes: ['a:read', 'b:read', 'c:read', 'd:read', 'e:read'], max: 1 },
};

export const Empty: Story = {
  args: { scopes: [] },
};
