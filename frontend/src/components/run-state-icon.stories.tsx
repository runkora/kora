import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@runkora/hades';
import { RunStateIcon } from './run-state-icon';
import { RUN_STATE_DISPLAY } from './run-state';

/** A run's state as one icon, labelled with the state for screen readers. */
const meta: Meta<typeof RunStateIcon> = {
  title: 'App/Atoms/RunStateIcon',
  component: RunStateIcon,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof RunStateIcon>;

export const Default: Story = { args: { state: 'active' } };

export const AllStates: Story = {
  render: () => (
    <Stack direction="horizontal" gap="cozy">
      {Object.keys(RUN_STATE_DISPLAY).map((state) => (
        <RunStateIcon key={state} state={state} />
      ))}
    </Stack>
  ),
};
