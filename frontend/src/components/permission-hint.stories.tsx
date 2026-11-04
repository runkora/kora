import type { Meta, StoryObj } from '@storybook/react';
import { Button, Icon } from '@runkora/hades';
import { PermissionHint } from './permission-hint';

/**
 * PermissionHint
 *
 * Wraps a control the caller cannot use. The control stays on screen, disabled, and hovering or
 * focusing it says which permission is missing.
 */
const meta: Meta<typeof PermissionHint> = {
  title: 'App/Molecules/PermissionHint',
  component: PermissionHint,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof PermissionHint>;

export const Denied: Story = {
  args: { allowed: false, action: 'start runs of this agent' },
  render: (args) => (
    <PermissionHint {...args}>
      <Button size="sm" disabled={!args.allowed}>
        <Icon name="play" size="xs" />
        Run
      </Button>
    </PermissionHint>
  ),
};

export const Allowed: Story = {
  ...Denied,
  args: { allowed: true, action: 'start runs of this agent' },
};
