import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@orbit/terra';
import { Example } from '../packages/terra/src/docs/story-ui';
import { ThemeToggle } from './theme-toggle';

/**
 * ThemeToggle
 *
 * An icon button that switches between the light and dark theme. It sits in the dashboard header.
 */
const meta: Meta<typeof ThemeToggle> = {
  title: 'App/Atoms/ThemeToggle',
  component: ThemeToggle,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof ThemeToggle>;

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Header control">
        <Stack direction="horizontal" gap="none">
          <ThemeToggle />
        </Stack>
      </Example>
    </Stack>
  ),
};
