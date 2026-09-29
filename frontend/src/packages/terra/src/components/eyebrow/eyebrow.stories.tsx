import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Eyebrow } from './eyebrow';

const meta: Meta<typeof Eyebrow> = {
  title: 'Terra/Atoms/Eyebrow',
  component: Eyebrow,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Eyebrow>;

export const Default: Story = {
  args: { children: 'Audit' },
};

/** An eyebrow opening a small card section, followed by supporting copy. */
export const AboveSupportingText: Story = {
  render: () => (
    <Stack gap="tight" maxWidth="card">
      <Eyebrow>Audit</Eyebrow>
      <Text size="micro" color="subtle">
        Saving re-provisions the agent and recompiles its grants before they apply.
      </Text>
    </Stack>
  ),
};

/** Multiple eyebrows as section dividers in a rail. */
export const AsSectionDividers: Story = {
  render: () => (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Eyebrow>Planned limits</Eyebrow>
        <Text size="supporting" color="muted">
          Requests per hour, spend per day.
        </Text>
      </Stack>
      <Stack gap="tight">
        <Eyebrow>Connections</Eyebrow>
        <Text size="supporting" color="muted">
          Outbound hosts this agent may reach.
        </Text>
      </Stack>
    </Stack>
  ),
};
