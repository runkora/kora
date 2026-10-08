import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@orbit/hades';
import { Example, Option, OptionTable } from '../packages/hades/src/docs/story-ui';
import { StatusBadge } from './status-badge';

/**
 * StatusBadge
 *
 * A small badge with a coloured dot that shows the health of an agent or resource. Use it wherever
 * a status needs to be readable at a glance.
 */
const meta: Meta<typeof StatusBadge> = {
  title: 'App/Atoms/StatusBadge',
  component: StatusBadge,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof StatusBadge>;

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Agent health">
        <Stack direction="horizontal" gap="snug" align="center">
          <StatusBadge status="healthy" />
          <StatusBadge status="degraded" />
          <StatusBadge status="error" />
        </Stack>
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="status">
        <Option name="healthy" use="Working as expected.">
          <StatusBadge status="healthy" />
        </Option>
        <Option name="degraded" use="Working, but slower or partly failing.">
          <StatusBadge status="degraded" />
        </Option>
        <Option name="error" use="Not working and needs attention.">
          <StatusBadge status="error" />
        </Option>
        <Option name="idle" use="Running with nothing to do.">
          <StatusBadge status="idle" />
        </Option>
        <Option name="pending" use="Waiting to start or to be approved.">
          <StatusBadge status="pending" />
        </Option>
        <Option name="unknown" use="No status has been reported yet. Shown muted.">
          <StatusBadge status="unknown" />
        </Option>
      </OptionTable>
      <OptionTable title="label">
        <Option name="label" use="Replaces the default text when the status needs a specific word.">
          <StatusBadge status="healthy" label="Running" />
        </Option>
      </OptionTable>
    </Stack>
  ),
};
