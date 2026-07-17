'use client';

import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Stack } from '@orbit/terra';
import type { ConnectionCatalogEntry, Connector } from '@orbit/shared/api';
import { ConnectionCards } from './connection-cards';

const meta: Meta<typeof ConnectionCards> = {
  title: 'App/Shared/ConnectionCards',
  component: ConnectionCards,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof ConnectionCards>;

const SAMPLE_CONNECTIONS: ConnectionCatalogEntry[] = [
  {
    name: 'github',
    adapter: 'github_app',
    actions: ['repo:read', 'repo:write'],
  },
  {
    name: 'slack',
    adapter: 'slack_bot',
    actions: ['chat:write', 'channels:read', 'users:read'],
  },
];

function Controlled({ initialValue = [] }: { initialValue?: Connector[] }) {
  const [value, setValue] = useState<Connector[]>(initialValue);
  return (
    <Stack maxWidth="card">
      <ConnectionCards available={SAMPLE_CONNECTIONS} value={value} onChange={setValue} />
    </Stack>
  );
}

export const Default: Story = {
  render: () => <Controlled />,
};

export const PartiallyGranted: Story = {
  render: () => <Controlled initialValue={[{ connection: 'github', actions: ['repo:read'] }]} />,
};

export const FullyGranted: Story = {
  render: () => (
    <Controlled
      initialValue={[
        { connection: 'github', actions: ['repo:read', 'repo:write'] },
        { connection: 'slack', actions: ['chat:write', 'channels:read', 'users:read'] },
      ]}
    />
  ),
};

export const NoConnections: Story = {
  render: () => {
    return <ConnectionCards available={[]} value={[]} onChange={() => {}} />;
  },
};
