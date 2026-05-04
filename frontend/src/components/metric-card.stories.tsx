import type { Meta, StoryObj } from '@storybook/react';
import { Icon } from '@orbit/terra';
import { MetricCard } from './metric-card';

const meta: Meta<typeof MetricCard> = {
  title: 'App/Shared/MetricCard',
  component: MetricCard,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof MetricCard>;

export const Default: Story = {
  args: {
    icon: <Icon name="activity" />,
    label: 'Total Requests',
    value: '4.8K',
  },
};

export const WithDelta: Story = {
  args: {
    icon: <Icon name="activity" />,
    label: 'Total Requests',
    value: '4.8K',
    delta: { text: '▲ 18.2% vs last 7 days', positive: true },
  },
};

export const WithDeltaNegative: Story = {
  args: {
    icon: <Icon name="alert-circle" />,
    label: 'Error Rate',
    value: '2.3%',
    delta: { text: '▼ 0.8% vs last 7 days', positive: false, positiveIsBad: true },
  },
};

export const WithBreakdown: Story = {
  args: {
    icon: <Icon name="shield-check" />,
    label: 'Grants',
    value: 124,
    href: '/policy/grants',
    breakdown: [
      { label: 'AGENT', value: 89 },
      { label: 'HUMAN', value: 24 },
      { label: 'PAT', value: 11 },
    ],
  },
};
