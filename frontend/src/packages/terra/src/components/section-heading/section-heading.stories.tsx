import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { SectionHeading } from './section-heading';

const meta: Meta<typeof SectionHeading> = {
  title: 'Terra/Atoms/SectionHeading',
  component: SectionHeading,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof SectionHeading>;

export const Default: Story = {
  args: {
    icon: 'user',
    title: 'Identity',
    description: 'Name your agent and describe what it does.',
  },
};

export const WithoutDescription: Story = {
  args: {
    icon: 'shield',
    title: 'Guardrails',
  },
};

/** A sequence of section headings, as they appear down a builder step. */
export const BuilderSteps: Story = {
  render: () => (
    <Stack gap="relaxed" maxWidth="prose">
      <SectionHeading
        icon="bot"
        title="Behaviour"
        description="Choose the model and write the system prompt."
      />
      <SectionHeading
        icon="wrench"
        title="Tools & access"
        description="Grant the MCP servers and scopes this agent can use."
      />
      <SectionHeading
        icon="network"
        title="Network access"
        description="Declare the connectors and egress mode."
      />
    </Stack>
  ),
};
