'use client';

import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Stack } from '@runkora/hades';
import { Example, Option, OptionTable } from '../packages/hades/src/docs/story-ui';
import { ScopePicker } from './scope-picker';
import type { ScopeOption } from './scope-option';

/**
 * ScopePicker
 *
 * Choose scopes grouped by resource type, then by server or connection. Each level has a checkbox that selects or clears
 * all of its scopes, and search matches scope strings and descriptions. Built for vocabularies of several hundred scopes.
 */
const meta: Meta<typeof ScopePicker> = {
  title: 'App/Molecules/ScopePicker',
  component: ScopePicker,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof ScopePicker>;

const SAMPLE_SCOPES = [
  'agents:read',
  'agents:write',
  'agents:runs:read',
  'agents:runs:write',
  'inference:chat',
  'inference:embeddings',
  'mcp:github:read',
  'mcp:github:write',
  'connection:github',
  'egress:api.github.com',
];

const DESCRIBED_SCOPES: ScopeOption[] = [
  {
    scope: 'mcp:github:issues.create:use',
    description: 'Open a GitHub issue',
    resourceType: 'mcp',
    path: ['github', 'issues', 'create'],
  },
  {
    scope: 'mcp:github:pulls.review:use',
    description: 'Review a pull request',
    resourceType: 'mcp',
    path: ['github', 'pulls', 'review'],
  },
  {
    scope: 'mcp:slack:chat.post:use',
    description: 'Post a message to a channel',
    resourceType: 'mcp',
    path: ['slack', 'chat', 'post'],
  },
  { scope: 'mcp:invoke', description: 'Reach the tool gateway', resourceType: 'mcp', path: [] },
  {
    scope: 'connection:github:repo:read',
    description: 'Read repository contents',
    resourceType: 'connection',
    path: ['github', 'repo'],
  },
  {
    scope: 'connection:github:repo:write',
    description: 'Push to repositories',
    resourceType: 'connection',
    path: ['github', 'repo'],
  },
  {
    scope: 'agents:runs:read',
    description: 'Read agent runs',
    resourceType: 'agents',
    path: ['runs'],
  },
  {
    scope: 'inference:chat:use',
    description: 'Call chat models',
    resourceType: 'inference',
    path: ['chat'],
  },
];

/** About the size of a production vocabulary: 383 scopes, most of them MCP tools. */
const LARGE_SCOPES: ScopeOption[] = [
  ...Array.from({ length: 265 }, (_, i) => ({
    scope: `mcp:server${i % 25}:tool${i}:use`,
    description: `Run tool ${i} on server ${i % 25}`,
    resourceType: 'mcp',
    path: [`server${i % 25}`, `tool${i}`],
  })),
  ...Array.from({ length: 49 }, (_, i) => ({
    scope: `connection:conn${i % 7}:category:item${i}:read`,
    description: `Read item ${i} on connection ${i % 7}`,
    resourceType: 'connection',
    path: [`conn${i % 7}`, 'category'],
  })),
  ...Array.from({ length: 31 }, (_, i) => ({
    scope: `agents:area${i % 4}:action${i}`,
    description: `Agent action ${i}`,
    resourceType: 'agents',
    path: [`area${i % 4}`],
  })),
  ...Array.from({ length: 23 }, (_, i) => ({
    scope: `inference:model${i}:use`,
    description: `Call model ${i}`,
    resourceType: 'inference',
    path: [`model${i}`],
  })),
  ...Array.from({ length: 15 }, (_, i) => ({
    scope: `platform:thing${i}:read`,
    description: `Read platform thing ${i}`,
    resourceType: 'platform',
    path: [`thing${i}`],
  })),
];

function Controlled({
  initial = [],
  searchable,
  options,
}: {
  initial?: string[];
  searchable?: boolean;
  options?: ScopeOption[];
}) {
  const [selected, setSelected] = useState<string[]>(initial);
  return (
    <Stack maxWidth="card">
      <ScopePicker
        options={options}
        scopes={SAMPLE_SCOPES}
        selected={selected}
        onChange={setSelected}
        searchable={searchable}
      />
    </Stack>
  );
}

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Grant scopes">
        <Controlled initial={['inference:chat']} />
      </Example>
    </Stack>
  ),
};

export const WithDescriptions: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Hierarchy and descriptions">
        <Controlled options={DESCRIBED_SCOPES} initial={['connection:github:repo:read']} />
      </Example>
    </Stack>
  ),
};

export const LargeVocabulary: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="383 scopes, collapsed by default">
        <Controlled options={LARGE_SCOPES} />
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="Options">
        <Option name="default" use="Every group is listed. Open a group to pick single scopes.">
          <Controlled />
        </Option>
        <Option
          name="searchable"
          use="On by default. Matches scope strings and descriptions; groups with matches open while you type. Pass false to hide it."
        >
          <Controlled searchable />
        </Option>
        <Option
          name="selected"
          use="Scopes already chosen. A group with only some chosen shows a partial checkbox."
        >
          <Controlled initial={['agents:read', 'inference:chat', 'inference:embeddings']} />
        </Option>
      </OptionTable>
    </Stack>
  ),
};
