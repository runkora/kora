import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@runkora/hades';
import { TenantPicker } from './tenant-picker';

/** The sign-in picker shown when the person's claims match several tenants. */
const meta: Meta<typeof TenantPicker> = {
  title: 'Pages/Sign-in/Tenant picker',
  component: TenantPicker,
  parameters: { layout: 'centered' },
  args: { onPick: () => undefined },
  decorators: [
    (Story) => (
      <Stack maxWidth="card">
        <Story />
      </Stack>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof TenantPicker>;

const TWO = [
  { id: 'kora-tenant-example', displayName: 'Example Platform' },
  { id: 'kora-tenant-example-data', displayName: 'Example Data' },
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
    ].map((name) => ({
      id: `kora-tenant-example-${name.toLowerCase()}`,
      displayName: `Example ${name}`,
    })),
  },
};

export const LongNames: Story = {
  args: {
    tenants: [
      {
        id: 'kora-tenant-example',
        displayName: 'Example Platform Engineering and Developer Experience',
      },
      {
        id: 'kora-tenant-example-data',
        displayName: 'Example Data Platform, Analytics and Machine Learning Infrastructure',
      },
    ],
  },
};

/** After a pick, while the sign-in for that tenant runs. */
export const Picking: Story = { args: { tenants: TWO, pickingId: 'kora-tenant-example-data' } };
