import type { Meta, StoryObj } from '@storybook/react';
import { StatusBadge, type OrbitStatus } from './status-badge';

const meta: Meta<typeof StatusBadge> = {
  title: 'App/Shared/StatusBadge',
  component: StatusBadge,
  tags: ['autodocs'],
  argTypes: {
    status: {
      control: 'select',
      options: ['healthy', 'degraded', 'error', 'idle', 'pending', 'unknown'] satisfies OrbitStatus[],
    },
  },
};
export default meta;
type Story = StoryObj<typeof StatusBadge>;

export const Default: Story = { args: { status: 'healthy' } };
export const Degraded: Story = { args: { status: 'degraded' } };
export const Error: Story = { args: { status: 'error' } };
export const Idle: Story = { args: { status: 'idle' } };
export const Pending: Story = { args: { status: 'pending' } };
export const Unknown: Story = { args: { status: 'unknown' } };
export const WithLabel: Story = { args: { status: 'degraded', label: 'Bundle stale' } };
