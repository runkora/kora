import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@runkora/hades';
import { TenantList } from './tenant-list';

/**
 * TenantList
 *
 * The person's tenants as pickable rows, used by the account menu and the MCP consent Switch. The
 * owner tracks the switch with `useTenantSwitch` and passes its state in.
 */
const meta: Meta<typeof TenantList> = {
  title: 'App/Molecules/TenantList',
  component: TenantList,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
  args: { switching: null, failed: false, onPick: () => {} },
  decorators: [
    (Story) => (
      <Stack maxWidth="form-field">
        <Story />
      </Stack>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof TenantList>;

const TWO = [
  { id: 'kora-tenant-example', displayName: 'Example Platform', active: true },
  { id: 'kora-tenant-example-data', displayName: 'Example Data', active: false },
];

export const TwoTenants: Story = { args: { tenants: TWO } };

export const ManyTenants: Story = {
  args: {
    tenants: [
      'Platform',
      'Data',
      'Machine Learning',
      'Payments',
      'Search',
      'Mobile',
      'Security',
      'Growth',
    ].map((name, i) => ({ id: `t-${i}`, displayName: `Example ${name}`, active: i === 0 })),
  },
};
