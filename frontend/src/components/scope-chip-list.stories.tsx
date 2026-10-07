import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@orbit/terra';
import { Example, Option, OptionTable } from '../packages/terra/src/docs/story-ui';
import { ScopeChipList } from './scope-chip-list';

/**
 * ScopeChipList
 *
 * A row of scope chips that shows a few and tucks the rest behind a "+N more" button. Use it in a
 * table cell or a card where the number of scopes is not fixed.
 */
const meta: Meta<typeof ScopeChipList> = {
  title: 'App/Molecules/ScopeChipList',
  component: ScopeChipList,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof ScopeChipList>;

const FEW = ['inference:invoke', 'agents:runs:read'];

const MANY = [
  'inference:invoke',
  'inference:anthropic:claude-sonnet:use',
  'connection:github:metadata:read',
  'connection:github:contents:read',
  'connection:github:contents:write',
  'egress:github.com:connect',
];

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose" gap="relaxed">
      <Example title="A few scopes">
        <ScopeChipList scopes={FEW} />
      </Example>
      <Example title="Too many to show">
        <ScopeChipList scopes={MANY} />
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="Options">
        <Option name="scopes" use="Every scope to show. Empty shows nothing.">
          <ScopeChipList scopes={FEW} />
        </Option>
        <Option name="max" use="How many chips show before +N more. The default is 3.">
          <ScopeChipList scopes={MANY} max={1} />
        </Option>
        <Option
          name="maxRows"
          use="Instead of a count, show as many chips as fit in this many rows."
        >
          <Stack maxWidth="card">
            <ScopeChipList scopes={MANY} maxRows={1} />
          </Stack>
        </Option>
        <Option name="empty" use="No scopes.">
          <ScopeChipList scopes={[]} />
        </Option>
      </OptionTable>
    </Stack>
  ),
};
