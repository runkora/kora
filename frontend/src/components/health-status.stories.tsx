/**
 * HealthStatus renders live: it fetches from /api/identity/health on mount.
 * In Storybook (without the BFF running) the component will cycle through
 * its loading → error states automatically. This story documents the
 * component shape and renders it in the correct layout context.
 */
import type { Meta, StoryObj } from '@storybook/react';
import { HealthStatus } from './health-status';

const meta: Meta<typeof HealthStatus> = {
  title: 'App/Shared/HealthStatus',
  component: HealthStatus,
  tags: ['autodocs'],
  parameters: {
    docs: {
      description: {
        component:
          'Fetches backend health from the BFF `/api/identity/health` route on mount. ' +
          'In Storybook (no BFF) the component renders its error state. ' +
          'The loading → ok → error transitions are all shown in the component source.',
      },
    },
  },
};
export default meta;
type Story = StoryObj<typeof HealthStatus>;

export const Default: Story = {};
