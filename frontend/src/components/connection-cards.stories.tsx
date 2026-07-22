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
    actions: ['contents:read', 'contents:write', 'issues:read', 'issues:write'],
    baseline: ['metadata:read'],
    qualifier: {
      name: 'repository',
      description: 'Narrow the grant to specific repositories of the installation.',
      ceiling: ['webapp', 'orbit', 'infra'],
    },
  },
  {
    name: 'slack',
    adapter: 'slack_bot',
    actions: ['chat:write', 'channels:read', 'users:read'],
    baseline: [],
  },
];

/** A qualifier-capable connection whose config declares no ceiling allowlist. */
const NO_CEILING_CONNECTIONS: ConnectionCatalogEntry[] = [
  {
    name: 'github-open',
    adapter: 'github_app',
    actions: ['contents:read', 'issues:write'],
    baseline: ['metadata:read'],
    qualifier: {
      name: 'repository',
      description: 'Narrow the grant to specific repositories of the installation.',
      ceiling: [],
    },
  },
];

function Controlled({
  available = SAMPLE_CONNECTIONS,
  initialValue = [],
}: {
  available?: ConnectionCatalogEntry[];
  initialValue?: Connector[];
}) {
  const [value, setValue] = useState<Connector[]>(initialValue);
  return (
    <Stack maxWidth="card">
      <ConnectionCards available={available} value={value} onChange={setValue} />
    </Stack>
  );
}

export const Default: Story = {
  render: () => <Controlled />,
};

export const PartiallyGranted: Story = {
  render: () => (
    <Controlled initialValue={[{ connection: 'github', actions: ['contents:read'] }]} />
  ),
};

export const FullyGranted: Story = {
  render: () => (
    <Controlled
      initialValue={[
        {
          connection: 'github',
          actions: ['contents:read', 'contents:write', 'issues:read', 'issues:write'],
        },
        { connection: 'slack', actions: ['chat:write', 'channels:read', 'users:read'] },
      ]}
    />
  ),
};

/** Qualifier-capable connection granted with no resource narrowing — the
 * "All repositories" default. Expand the card to see the resource scope block
 * and the implied baseline row. */
export const AllResources: Story = {
  render: () => (
    <Controlled
      initialValue={[{ connection: 'github', actions: ['issues:read', 'issues:write'] }]}
    />
  ),
};

/** Resources selected within the connection's ceiling — action rows preview the
 * qualified scope form and the header summary gains the repository count. */
export const SelectedResources: Story = {
  render: () => (
    <Controlled
      initialValue={[
        {
          connection: 'github',
          actions: ['contents:read', 'issues:write'],
          resources: ['webapp', 'orbit'],
        },
      ]}
    />
  ),
};

/** A selected resource outside the connection's declared ceiling allowlist —
 * rendered with the warning treatment (requests against it will be denied). */
export const OutOfCeilingResource: Story = {
  render: () => (
    <Controlled
      initialValue={[
        {
          connection: 'github',
          actions: ['issues:write'],
          resources: ['webapp', 'renamed-repo'],
        },
      ]}
    />
  ),
};

/** Ceiling-less qualifier: manual entry only, and no out-of-ceiling warnings
 * since there is no reference set to compare against. */
export const NoCeiling: Story = {
  render: () => (
    <Controlled
      available={NO_CEILING_CONNECTIONS}
      initialValue={[
        { connection: 'github-open', actions: ['contents:read'], resources: ['some-repo'] },
      ]}
    />
  ),
};

export const NoConnections: Story = {
  render: () => {
    return <ConnectionCards available={[]} value={[]} onChange={() => {}} />;
  },
};
