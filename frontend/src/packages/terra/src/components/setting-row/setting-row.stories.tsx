import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Badge } from '../badge/badge';
import { SettingRow } from './setting-row';

const meta: Meta<typeof SettingRow> = {
  title: 'Terra/Molecules/SettingRow',
  component: SettingRow,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof SettingRow>;

/** Label, description, and the configured value. */
export const Default: Story = {
  render: () => (
    <div className="w-[440px]">
      <SettingRow
        label="Default model"
        description="Used when a run does not pin one."
        value="claude-sonnet-4-5"
      />
    </div>
  ),
};

/** Without a description the row collapses to a single line. */
export const LabelOnly: Story = {
  render: () => (
    <div className="w-[440px]">
      <SettingRow label="Region" value="ap-southeast-2" />
    </div>
  ),
};

/** `value` is a ReactNode, so a Badge or any other element works in the chip. */
export const NodeValue: Story = {
  render: () => (
    <div className="w-[440px]">
      <SettingRow
        label="Egress policy"
        description="Applied to every outbound call the agent makes."
        value={<Badge variant="healthy">enforced</Badge>}
      />
    </div>
  ),
};

/** A settings pane: rows stack with a consistent container and aligned chips. */
export const Stacked: Story = {
  render: () => (
    <div className="w-[440px]">
      <Stack gap="snug">
        <SettingRow
          label="Default model"
          description="Used when a run does not pin one."
          value="claude-sonnet-4-5"
        />
        <SettingRow label="Max tokens" description="Ceiling per completion request." value="8192" />
        <SettingRow
          label="Timeout"
          description="Runs are terminated past this duration."
          value="15m"
        />
        <SettingRow label="Region" value="ap-southeast-2" />
      </Stack>
    </div>
  ),
};
