import type { Meta, StoryObj } from '@storybook/react';
import { CreateGrantWizard } from './create-grant-wizard';
import { AVAILABLE_SCOPES } from './policy-mock-data';

/**
 * The human-only create wizard: user, scopes, an optional budget, review, then a confirmation. It
 * offers no other principal type.
 */
const meta: Meta<typeof CreateGrantWizard> = {
  title: 'Pages/Policy/Create grant wizard',
  component: CreateGrantWizard,
  parameters: { layout: 'fullscreen' },
  args: { availableScopes: AVAILABLE_SCOPES, onClose: () => undefined, onCreated: () => undefined },
};

export default meta;
type Story = StoryObj<typeof CreateGrantWizard>;

export const Default: Story = {};
