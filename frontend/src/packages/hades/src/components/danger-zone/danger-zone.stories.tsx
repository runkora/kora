import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Stack } from '../layout/stack';
import { DangerZone } from './danger-zone';

/**
 * DangerZone
 *
 * A red row at the bottom of a settings page for one destructive action. Give each action its own row.
 */
const meta: Meta<typeof DangerZone> = {
  title: 'Hades/Molecules/DangerZone',
  component: DangerZone,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof DangerZone>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl">
      <Example title="Two destructive actions">
        <Stack gap="snug">
          <DangerZone
            title="Rotate signing key"
            description="Every issued token stops working straight away."
            action={
              <Button variant="destructive" size="xs">
                Rotate key
              </Button>
            }
          />
          <DangerZone
            title="Delete this tenant"
            description="Removes all agents, grants and audit history. This cannot be undone."
            action={
              <Button variant="destructive" size="xs">
                Delete tenant
              </Button>
            }
          />
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="default" use="A title, what is lost, and the button that does it.">
          <DangerZone
            title="Delete this agent"
            description="Removes the agent and all run history. This cannot be undone."
            action={
              <Button variant="destructive" size="xs">
                Delete agent
              </Button>
            }
          />
        </Option>
        <Option name="no description" use="Leave it out when the result is obvious.">
          <DangerZone
            title="Revoke all tokens"
            action={
              <Button variant="destructive" size="xs">
                Revoke
              </Button>
            }
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
