import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { CodeBlock } from './code-block';

const meta: Meta<typeof CodeBlock> = {
  title: 'Terra/Molecules/CodeBlock',
  component: CodeBlock,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
  argTypes: {
    wrap: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof CodeBlock>;

export const Default: Story = {
  args: {
    text: 'orbit agents deploy apollo-3 --env production',
  },
  render: (args) => (
    <Stack maxWidth="prose">
      <CodeBlock {...args} />
    </Stack>
  ),
};

/** A filename header bar renders above the block, visually merged. */
export const WithFilename: Story = {
  args: {
    text: 'export ORBIT_API_KEY="sk-orbit-xxx"\nexport ORBIT_TENANT="acme"',
    filename: '~/.zshrc',
  },
  render: (args) => (
    <Stack maxWidth="prose">
      <CodeBlock {...args} />
    </Stack>
  ),
};

/** `density="compact"` tightens padding and type for dense contexts. */
export const Compact: Story = {
  args: {
    text: '{"action":"opened","number":108,"pull_request":{"number":108,"title":"Change 8","base":{"ref":"main"}},"repository":{"full_name":"clearroute/orbit"}}',
    density: 'compact',
    wrap: true,
  },
};

/** `wrap` soft-wraps long prose-like content instead of scrolling horizontally. */
export const Wrapped: Story = {
  args: {
    text: 'You are a release-notes agent. Summarise the merged pull requests since the last tag, group them by area, and never include internal ticket identifiers in the published output.',
    filename: 'system-prompt.txt',
    wrap: true,
  },
  render: (args) => (
    <Stack maxWidth="prose">
      <CodeBlock {...args} />
    </Stack>
  ),
};
