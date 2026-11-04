import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { GrantsTab } from './grants-tab';
import {
  AVAILABLE_SCOPES,
  GRANTS,
  MANY_GRANTS,
  PAT_ATTRIBUTION,
  USER_ATTRIBUTION,
} from './policy-mock-data';
import { policyParameters, withPolicy } from './policy-story-setup';

/**
 * Only a person's grant can be created or deleted here. Agent, trigger and token grants stay in
 * the list, read-only, with a link to the agent, trigger or token that owns them.
 */
const meta: Meta<typeof GrantsTab> = {
  title: 'Pages/Policy/Grants',
  component: GrantsTab,
  parameters: policyParameters('/policy/grants'),
  decorators: withPolicy(),
  args: {
    initialGrants: GRANTS,
    availableScopes: AVAILABLE_SCOPES,
    patAttribution: PAT_ATTRIBUTION,
    userAttribution: USER_ATTRIBUTION,
    initialError: null,
    page: 1,
    limit: 50,
    total: GRANTS.length,
  },
};

export default meta;
type Story = StoryObj<typeof GrantsTab>;

export const Default: Story = {};

export const Many: Story = {
  args: { initialGrants: MANY_GRANTS.slice(0, 20), total: MANY_GRANTS.length },
};

export const Empty: Story = {
  args: { initialGrants: [], total: 0 },
};

/** The create wizard opened from the header button. */
export const CreateWizard: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByRole('button', { name: /create grant/i }));
  },
};
