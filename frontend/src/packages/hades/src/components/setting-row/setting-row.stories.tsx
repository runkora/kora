import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Badge } from '../badge/badge';
import { Stack } from '../layout/stack';
import { SettingRow } from './setting-row';

/**
 * SettingRow
 *
 * Shows one setting as a name, an optional note and its current value. It is read-only.
 */
const meta: Meta<typeof SettingRow> = {
  title: 'Hades/Molecules/SettingRow',
  component: SettingRow,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof SettingRow>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-lg">
      <Example title="A settings list">
        <Stack gap="snug">
          <SettingRow
            label="Default model"
            description="Used when a run does not pick one."
            value="claude-sonnet-4-5"
          />
          <SettingRow
            label="Max tokens"
            description="The most tokens one reply can use."
            value="8192"
          />
          <SettingRow label="Timeout" description="Runs stop after this long." value="15m" />
          <SettingRow label="Region" value="ap-southeast-2" />
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="default" use="A name and its value.">
          <SettingRow label="Region" value="ap-southeast-2" />
        </Option>
        <Option name="description" use="A short note under the name.">
          <SettingRow
            label="Default model"
            description="Used when a run does not pick one."
            value="claude-sonnet-4-5"
          />
        </Option>
        <Option name="value as an element" use="The value can be a Badge or other element.">
          <SettingRow
            label="Egress policy"
            description="Applies to every outbound call."
            value={<Badge variant="healthy">enforced</Badge>}
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
