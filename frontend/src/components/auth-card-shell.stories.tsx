import type { Meta, StoryObj } from '@storybook/react';
import { Stack, Text } from '@runkora/hades';
import { AuthCardShell } from './auth-card-shell';

/** The sign-in card: the brand panel, a title and description, content and a footer strip. */
const meta: Meta<typeof AuthCardShell> = {
  title: 'App/Organisms/AuthCardShell',
  component: AuthCardShell,
  tags: ['autodocs'],
  parameters: { layout: 'fullscreen' },
  decorators: [
    (Story) => (
      <Stack align="center" justify="center" minHeight="screen" padding="comfortable">
        <Story />
      </Stack>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof AuthCardShell>;

export const Default: Story = {
  args: {
    title: 'Sign in',
    description: 'Use your work email.',
    footer: (
      <Text size="micro" color="subtle">
        Footer text
      </Text>
    ),
    children: <Text size="supporting">Card content</Text>,
  },
};
