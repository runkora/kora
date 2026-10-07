import type { Meta, StoryObj } from '@storybook/react';
import { RoutesTab } from './routes-tab';
import { ROUTE_REGISTRATIONS } from './policy-mock-data';
import { policyParameters, withPolicy } from './policy-story-setup';

const meta: Meta<typeof RoutesTab> = {
  title: 'Pages/Policy/Routes',
  component: RoutesTab,
  parameters: policyParameters('/policy/routes'),
  decorators: withPolicy(),
  args: { initialRegistrations: ROUTE_REGISTRATIONS, initialError: null },
};

export default meta;
type Story = StoryObj<typeof RoutesTab>;

export const Default: Story = {};

export const Empty: Story = {
  args: { initialRegistrations: [] },
};

export const LoadError: Story = {
  args: { initialRegistrations: [], initialError: 'policy-service unavailable (503)' },
};
