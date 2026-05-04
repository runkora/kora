import type { Meta, StoryObj } from '@storybook/react';
import { List } from '@orbit/terra';
import { EventFeedItem } from './event-feed-item';

const meta: Meta<typeof EventFeedItem> = {
  title: 'App/Shared/EventFeedItem',
  component: EventFeedItem,
  tags: ['autodocs'],
  decorators: [
    (Story) => (
      <List separator>
        <Story />
      </List>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof EventFeedItem>;

export const Default: Story = {
  args: {
    status: 'healthy',
    label: 'policy.allow',
    meta: 'apollo-retriever',
    message: 'Tool call approved — read_file within scope',
    timestamp: '09:41:12',
  },
};

export const Error: Story = {
  args: {
    status: 'error',
    label: 'policy.deny',
    meta: 'daedalus-planner',
    message: 'Tool call blocked — exec_shell outside allowed scope',
    timestamp: '09:40:55',
  },
};

export const Degraded: Story = {
  args: {
    status: 'degraded',
    label: 'agent.degraded',
    meta: 'hermes-dispatcher',
    message: 'Response latency exceeded P99 threshold (4.2 s)',
    timestamp: '09:40:50',
  },
};

export const NoMeta: Story = {
  args: {
    status: 'idle',
    label: 'agent.started',
    message: 'Agent bootstrap complete — awaiting first task',
    timestamp: '09:40:41',
  },
};
