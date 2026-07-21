import type { Meta, StoryObj } from '@storybook/react';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Alert } from './alert';

const meta: Meta<typeof Alert> = {
  title: 'Terra/Atoms/Alert',
  component: Alert,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
  argTypes: {
    variant: {
      control: 'select',
      options: ['info', 'warning', 'error'],
    },
  },
};

export default meta;
type Story = StoryObj<typeof Alert>;

export const Info: Story = {
  render: () => (
    <Alert variant="info" className="w-80">
      <Icon name="info" size="xs" color="accent" />
      <Text size="caption">Model gateway is processing your request.</Text>
    </Alert>
  ),
};

export const Warning: Story = {
  render: () => (
    <Alert variant="warning" className="w-80">
      <Icon name="alert-circle" size="xs" color="status-degraded" />
      <Text size="caption">Weights must sum to 100%.</Text>
    </Alert>
  ),
};

export const Error: Story = {
  render: () => (
    <Alert variant="error" className="w-80">
      <Icon name="alert-circle" size="xs" color="status-error" />
      <Text size="caption" color="muted">
        Credential rotation failed — try again.
      </Text>
    </Alert>
  ),
};

/** All three variants stacked for comparison */
export const AllVariants: Story = {
  render: () => (
    <Stack direction="vertical" gap="snug" className="w-80">
      <Alert variant="info">
        <Icon name="info" size="xs" color="accent" />
        <Text size="caption">Model gateway is processing your request.</Text>
      </Alert>
      <Alert variant="warning">
        <Icon name="alert-circle" size="xs" color="status-degraded" />
        <Text size="caption">Weights must sum to 100%.</Text>
      </Alert>
      <Alert variant="error">
        <Icon name="alert-circle" size="xs" color="status-error" />
        <Text size="caption" color="muted">
          Credential rotation failed — try again.
        </Text>
      </Alert>
    </Stack>
  ),
};
