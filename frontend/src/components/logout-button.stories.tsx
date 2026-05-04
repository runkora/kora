/**
 * LogoutButton POSTs to /api/auth/logout on click then redirects.
 * In Storybook the fetch will fail (no BFF) but the button renders and
 * is interactive — it will not navigate out of Storybook.
 */
import type { Meta, StoryObj } from '@storybook/react';
import { LogoutButton } from './logout-button';

const meta: Meta<typeof LogoutButton> = {
  title: 'App/Shared/LogoutButton',
  component: LogoutButton,
  tags: ['autodocs'],
  parameters: {
    docs: {
      description: {
        component:
          'POSTs to `/api/auth/logout`, clears the session cookie server-side, then redirects to `/login`. ' +
          'In Storybook the fetch will fail silently — the button remains interactive and shows its pending state.',
      },
    },
  },
};
export default meta;
type Story = StoryObj<typeof LogoutButton>;

export const Default: Story = {};
