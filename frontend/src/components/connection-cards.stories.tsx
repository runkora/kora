'use client';

import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Stack } from '@orbit/terra';
import type { ConnectionCatalogEntry, Connector } from '@orbit/shared/api';
import { Example, Option, OptionTable } from '../packages/terra/src/docs/story-ui';
import { ConnectionCards } from './connection-cards';

/**
 * ConnectionCards
 *
 * Grant an agent actions on each connection, one collapsible card per connection. Some connections can also narrow the grant to chosen resources, such as repositories.
 */
const meta: Meta<typeof ConnectionCards> = {
  title: 'App/Organisms/ConnectionCards',
  component: ConnectionCards,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
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
    },
  },
  {
    name: 'slack',
    adapter: 'slack_bot',
    actions: ['chat:write', 'channels:read', 'users:read'],
    baseline: [],
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

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Agent connections">
        <Controlled
          initialValue={[{ connection: 'github', actions: ['contents:read', 'issues:read'] }]}
        />
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="Options">
        <Option name="none granted" use="Every card starts empty. Open one to pick actions.">
          <Controlled />
        </Option>
        <Option
          name="partly granted"
          use="Some actions chosen. The card checkbox shows a partial state."
        >
          <Controlled initialValue={[{ connection: 'github', actions: ['contents:read'] }]} />
        </Option>
        <Option name="fully granted" use="Every action chosen. The card checkbox is checked.">
          <Controlled
            initialValue={[
              { connection: 'slack', actions: ['chat:write', 'channels:read', 'users:read'] },
            ]}
          />
        </Option>
        <Option
          name="all resources"
          use="A connection that supports resource scoping, with no resources chosen. Open the card to see the scope block and the baseline row."
        >
          <Controlled
            initialValue={[{ connection: 'github', actions: ['issues:read', 'issues:write'] }]}
          />
        </Option>
        <Option
          name="selected resources"
          use="Resources chosen. Action rows show the narrowed scope and the header shows the count."
        >
          <Controlled
            initialValue={[
              {
                connection: 'github',
                actions: ['contents:read', 'issues:write'],
                resources: ['webapp', 'orbit'],
              },
            ]}
          />
        </Option>
        <Option name="available: []" use="No connections exist yet, so the empty state shows.">
          <Controlled available={[]} />
        </Option>
      </OptionTable>
    </Stack>
  ),
};
