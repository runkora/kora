import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@clear-route/hades';
import { Example, Option, OptionTable } from '../packages/hades/src/docs/story-ui';
import { ScopeChip } from './scope-chip';

/**
 * ScopeChip
 *
 * Shows one scope string with the resource type, the path and the action in different colours.
 * Use it to list the scopes a key, role or grant holds.
 */
const meta: Meta<typeof ScopeChip> = {
  title: 'App/Atoms/ScopeChip',
  component: ScopeChip,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof ScopeChip>;

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Scopes on a key">
        <Stack direction="horizontal" gap="snug" wrap>
          <ScopeChip scope="agents:runs:read" />
          <ScopeChip scope="inference:chat" />
          <ScopeChip scope="mcp:github:write" />
        </Stack>
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="scope">
        <Option name="type:action" use="Two parts: the resource type and the action.">
          <ScopeChip scope="inference:chat" />
        </Option>
        <Option
          name="type:path:action"
          use="Middle parts narrow the resource and are shown in a softer colour."
        >
          <ScopeChip scope="agents:runs:read" />
        </Option>
        <Option name="type:path:path:action" use="Any number of middle parts is allowed.">
          <ScopeChip scope="mcp:github:issues:write" />
        </Option>
      </OptionTable>
    </Stack>
  ),
};
