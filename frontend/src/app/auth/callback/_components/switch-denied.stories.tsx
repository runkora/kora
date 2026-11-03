import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@runkora/hades';
import { SwitchDenied } from './switch-denied';

/** Shown when identity refuses a tenant switch. The person stays in the current tenant. */
const meta: Meta<typeof SwitchDenied> = {
  title: 'Pages/Sign-in/Switch denied',
  component: SwitchDenied,
  parameters: { layout: 'centered' },
  args: { onContinue: () => undefined },
  decorators: [
    (Story) => (
      <Stack maxWidth="card">
        <Story />
      </Stack>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof SwitchDenied>;

export const Default: Story = {};
