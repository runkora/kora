import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { SectionHeading } from './section-heading';

/**
 * SectionHeading
 *
 * The title at the top of a builder step or settings section: an icon tile, a
 * title and a short description.
 */
const meta: Meta<typeof SectionHeading> = {
  title: 'Hades/Atoms/SectionHeading',
  component: SectionHeading,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof SectionHeading>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Steps down a page">
        <Stack gap="relaxed">
          <SectionHeading
            icon="bot"
            title="Behaviour"
            description="Choose the model and write the system prompt."
          />
          <SectionHeading
            icon="wrench"
            title="Tools and access"
            description="Pick the tools and scopes this agent can use."
          />
          <SectionHeading
            icon="network"
            title="Network access"
            description="Choose which outside services the agent can reach."
          />
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="icon and title" use="Both are required. The icon sits in a coloured tile.">
          <SectionHeading icon="shield" title="Guardrails" />
        </Option>
        <Option name="description" use="One line under the title saying what the section is for.">
          <SectionHeading
            icon="user"
            title="Identity"
            description="Name your agent and describe what it does."
          />
        </Option>
        <Option
          name="children"
          use="Content for the section. It sits below the heading and lines up with the title."
        >
          <SectionHeading icon="key" title="Credentials" description="Keys the agent can use.">
            <Text size="supporting">github-token</Text>
            <Text size="supporting">slack-token</Text>
          </SectionHeading>
        </Option>
      </OptionTable>
    </div>
  ),
};
