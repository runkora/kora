import type { Meta, StoryObj } from '@storybook/react';
import { ManagedInGitBadge, ManagedInGitCallout } from './managed-in-git';

/**
 * ManagedInGit
 *
 * The badge marks one item declared in the configuration repository. The callout heads a page
 * whose items are all read-only because the tenant takes its changes from the repository.
 */
const meta: Meta<typeof ManagedInGitCallout> = {
  title: 'App/Molecules/ManagedInGit',
  component: ManagedInGitCallout,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof ManagedInGitCallout>;

export const Callout: Story = {
  args: { subject: 'Agents in this tenant' },
};

export const Badge: Story = {
  render: () => <ManagedInGitBadge />,
};
