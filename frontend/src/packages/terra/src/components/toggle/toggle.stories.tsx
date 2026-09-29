import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Toggle } from './toggle';

const meta: Meta<typeof Toggle> = {
  title: 'Terra/Atoms/Toggle',
  component: Toggle,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
  argTypes: {
    checked: { control: 'boolean' },
    disabled: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof Toggle>;

function DefaultExample() {
  const [on, setOn] = useState(false);
  return (
    <Stack direction="horizontal" gap="snug" align="center">
      <Toggle checked={on} onChange={setOn} aria-label="Enable feature" />
      <Text size="caption" color="muted">
        {on ? 'Enabled' : 'Disabled'}
      </Text>
    </Stack>
  );
}

/** Interactive — click to switch the toggle */
export const Default: Story = {
  render: () => <DefaultExample />,
};

/** Checked — feature is active */
export const Checked: Story = {
  render: () => <Toggle checked onChange={() => {}} aria-label="Enable model" />,
};

/** Unchecked — feature is inactive */
export const Unchecked: Story = {
  render: () => <Toggle checked={false} onChange={() => {}} aria-label="Enable model" />,
};

/** Disabled states — cannot be interacted with */
export const Disabled: Story = {
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center">
      <Stack gap="tight" align="center">
        <Toggle checked={false} onChange={() => {}} disabled aria-label="Disabled off" />
        <Text size="micro" color="muted">
          off
        </Text>
      </Stack>
      <Stack gap="tight" align="center">
        <Toggle checked onChange={() => {}} disabled aria-label="Disabled on" />
        <Text size="micro" color="muted">
          on
        </Text>
      </Stack>
    </Stack>
  ),
};

function WithLabelExample() {
  const [on, setOn] = useState(true);
  return (
    <Stack direction="horizontal" gap="snug" align="center">
      <Toggle checked={on} onChange={setOn} aria-label="Enable model routing" />
      <Text size="body">Enable model routing</Text>
    </Stack>
  );
}

/** With an adjacent visible label */
export const WithLabel: Story = {
  render: () => <WithLabelExample />,
};
