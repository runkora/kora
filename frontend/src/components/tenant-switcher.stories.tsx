import type { Meta, StoryObj } from '@storybook/react';
import { TenantSwitcher } from './tenant-switcher';

/**
 * TenantSwitcher
 *
 * The active tenant's name, opening the tenants the person's claims matched at the last sign-in or
 * switch. It renders nothing for a person with one tenant.
 */
const meta: Meta<typeof TenantSwitcher> = {
  title: 'App/Molecules/TenantSwitcher',
  component: TenantSwitcher,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
  args: { onSelect: () => {} },
};
export default meta;
type Story = StoryObj<typeof TenantSwitcher>;

export const Default: Story = {
  args: {
    tenants: [
      { id: 'kora-tenant-example', displayName: 'Example Platform', active: true },
      { id: 'kora-tenant-example-data', displayName: 'Example Data', active: false },
      { id: 'kora-tenant-example-ml', displayName: 'Example Machine Learning', active: false },
    ],
  },
};

/** One tenant: nothing renders. */
export const SingleTenant: Story = {
  args: {
    tenants: [{ id: 'kora-tenant-example', displayName: 'Example Platform', active: true }],
  },
};
