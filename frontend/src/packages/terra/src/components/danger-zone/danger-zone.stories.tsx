import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Button } from '../button/button';
import { DangerZone } from './danger-zone';

const meta: Meta<typeof DangerZone> = {
  title: 'Terra/Molecules/DangerZone',
  component: DangerZone,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof DangerZone>;

/** The standard shape: what is destroyed, and the button that destroys it. */
export const Default: Story = {
  render: () => (
    <div className="w-[520px]">
      <DangerZone
        title="Delete this agent"
        description="Removes the agent and all run history. This cannot be undone."
        action={
          <Button variant="destructive" size="xs">
            Delete agent
          </Button>
        }
      />
    </div>
  ),
};

/** Title and action only, where the consequence is already obvious from context. */
export const TitleOnly: Story = {
  render: () => (
    <div className="w-[520px]">
      <DangerZone
        title="Revoke all tokens"
        action={
          <Button variant="destructive" size="xs">
            Revoke
          </Button>
        }
      />
    </div>
  ),
};

/** One action per zone: two destructive actions get two zones. */
export const TwoZones: Story = {
  render: () => (
    <div className="w-[520px]">
      <Stack gap="snug">
        <DangerZone
          title="Rotate signing key"
          description="Every issued token is invalidated immediately."
          action={
            <Button variant="destructive" size="xs">
              Rotate
            </Button>
          }
        />
        <DangerZone
          title="Delete this tenant"
          description="Removes all agents, grants, and audit history. This cannot be undone."
          action={
            <Button variant="destructive" size="xs">
              Delete tenant
            </Button>
          }
        />
      </Stack>
    </div>
  ),
};
