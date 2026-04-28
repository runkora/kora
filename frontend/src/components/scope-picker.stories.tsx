'use client';

import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Stack } from '@orbit/terra';
import { ScopePicker } from './scope-picker';

const meta: Meta<typeof ScopePicker> = {
  title: 'App/Shared/ScopePicker',
  component: ScopePicker,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof ScopePicker>;

const SAMPLE_SCOPES = [
  'models:read',
  'models:write',
  'models:delete',
  'platform:grants:read',
  'platform:grants:write',
  'platform:roles:read',
  'platform:roles:write',
  'mcp:github:read',
  'mcp:github:write',
];

function Controlled({ initialSelected = [] }: { initialSelected?: string[] }) {
  const [selected, setSelected] = useState<string[]>(initialSelected);
  const [search, setSearch] = useState('');
  return (
    <Stack maxWidth="card">
      <ScopePicker
        availableScopes={SAMPLE_SCOPES}
        selectedScopes={selected}
        search={search}
        onSearchChange={setSearch}
        onToggle={(scope, checked) =>
          setSelected((prev) => (checked ? [...prev, scope] : prev.filter((s) => s !== scope)))
        }
        onToggleGroup={(scopes, checked) =>
          setSelected((prev) => {
            const set = new Set(prev);
            scopes.forEach((s) => (checked ? set.add(s) : set.delete(s)));
            return Array.from(set);
          })
        }
      />
    </Stack>
  );
}

export const Default: Story = {
  render: () => <Controlled />,
};

export const WithPreselected: Story = {
  render: () => <Controlled initialSelected={['models:read', 'platform:grants:read']} />,
};

export const Empty: Story = {
  render: () => (
    <Stack maxWidth="card">
      <ScopePicker
        availableScopes={[]}
        selectedScopes={[]}
        search=""
        onSearchChange={() => {}}
        onToggle={() => {}}
        onToggleGroup={() => {}}
      />
    </Stack>
  ),
};
