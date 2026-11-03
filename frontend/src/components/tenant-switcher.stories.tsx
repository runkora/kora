import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
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

const TWO = [
  { id: 'kora-tenant-example', displayName: 'Example Platform', active: true },
  { id: 'kora-tenant-example-data', displayName: 'Example Data', active: false },
];

/** Opens the list and picks the second tenant. The popover renders outside the canvas. */
async function pickSecond(canvasElement: HTMLElement) {
  const page = within(canvasElement.ownerDocument.body);
  await userEvent.click(page.getByRole('button', { name: /switch tenant/ }));
  await userEvent.click(await page.findByRole('button', { name: 'Example Data' }));
}

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
    ].map((name, i) => ({
      id: `kora-tenant-example-${i}`,
      displayName: `Example ${name}`,
      active: i === 0,
    })),
  },
};

export const LongName: Story = {
  args: {
    tenants: [
      {
        id: 'kora-tenant-example',
        displayName: 'Example Platform Engineering and Developer Experience',
        active: true,
      },
      TWO[1]!,
    ],
  },
};

/** After a pick, while the redirect to the identity provider starts. */
export const Switching: Story = {
  args: { tenants: TWO, onSelect: () => new Promise<void>(() => {}) },
  play: ({ canvasElement }) => pickSecond(canvasElement),
};

/** Discovery failed, so the switch never started. */
export const SwitchFailed: Story = {
  args: { tenants: TWO, onSelect: () => Promise.reject(new Error('discovery failed')) },
  play: ({ canvasElement }) => pickSecond(canvasElement),
};
