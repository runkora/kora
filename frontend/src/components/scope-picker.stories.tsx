'use client';

import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Stack } from '@orbit/hades';
import { Example, Option, OptionTable } from '../packages/hades/src/docs/story-ui';
import { ScopePicker } from './scope-picker';

/**
 * ScopePicker
 *
 * Choose scopes grouped by resource. Each group has a checkbox that selects or clears all of its scopes.
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

function Controlled({ initial = [], searchable }: { initial?: string[]; searchable?: boolean }) {
  const [selected, setSelected] = useState<string[]>(initial);
  return (
    <Stack maxWidth="card">
      <ScopePicker
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

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="Options">
        <Option name="default" use="Every group is listed. Open a group to pick single scopes.">
          <Controlled />
        </Option>
        <Option
          name="searchable"
          use="Adds a filter box for long lists. Groups with matches open while you type."
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
