import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@runkora/hades';
import { FilterChip } from './filter-chip';

/**
 * FilterChip
 *
 * A pill filter button with a trailing count. Lay several out in a row above a card view; the page
 * owns which one is active.
 */
const meta: Meta<typeof FilterChip> = {
  title: 'App/Molecules/FilterChip',
  component: FilterChip,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof FilterChip>;

const FILTERS = [
  { value: 'all', label: 'All', count: 12 },
  { value: 'global', label: 'Global', count: 7 },
  { value: 'tenant', label: 'Tenant', count: 5 },
];

function Row() {
  const [active, setActive] = useState('all');
  return (
    <Stack direction="horizontal" align="center" gap="snug" wrap>
      {FILTERS.map((f) => (
        <FilterChip
          key={f.value}
          label={f.label}
          count={f.count}
          active={active === f.value}
          onClick={() => setActive(f.value)}
        />
      ))}
    </Stack>
  );
}

export const Default: Story = { render: () => <Row /> };

export const Mono: Story = {
  args: { label: 'agents:runs', count: 4, active: true, mono: true, onClick: () => {} },
};
