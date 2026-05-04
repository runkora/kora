import type { Meta, StoryObj } from '@storybook/react';
import { ScopeChip } from './scope-chip';

const meta: Meta<typeof ScopeChip> = {
  title: 'App/Shared/ScopeChip',
  component: ScopeChip,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof ScopeChip>;

export const ResourceAction: Story = {
  args: { scope: 'models:read' },
};

export const ThreePart: Story = {
  args: { scope: 'mcp:github:write' },
};

export const DeepPath: Story = {
  args: { scope: 'platform:grants:write' },
};

export const SingleSegment: Story = {
  args: { scope: 'admin' },
};
