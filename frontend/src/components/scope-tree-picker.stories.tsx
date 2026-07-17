'use client';

import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Stack } from '@orbit/terra';
import { ScopeTreePicker } from './scope-tree-picker';

const meta: Meta<typeof ScopeTreePicker> = {
  title: 'App/Shared/ScopeTreePicker',
  component: ScopeTreePicker,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof ScopeTreePicker>;

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

function Controlled({ initialSelected = [] }: { initialSelected?: string[] }) {
  const [selected, setSelected] = useState<string[]>(initialSelected);
  return (
    <Stack maxWidth="card">
      <ScopeTreePicker scopes={SAMPLE_SCOPES} selected={selected} onChange={setSelected} />
    </Stack>
  );
}

export const Default: Story = {
  render: () => <Controlled />,
};

export const PartiallySelected: Story = {
  render: () => <Controlled initialSelected={['agents:read', 'agents:runs:read']} />,
};

export const FullySelectedGroup: Story = {
  render: () => <Controlled initialSelected={['inference:chat', 'inference:embeddings']} />,
};
