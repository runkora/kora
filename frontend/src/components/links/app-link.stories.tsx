import type { Meta, StoryObj } from '@storybook/react';
import { Stack, Text } from '@clear-route/hades';
import { Example, Option, OptionTable } from '../../packages/hades/src/docs/story-ui';
import { AppLink } from './app-link';

/**
 * AppLink
 *
 * A link to another page inside kora. It only accepts routes from the route list, so a
 * mistyped path fails at build time.
 */
const meta: Meta<typeof AppLink> = {
  title: 'App/Atoms/AppLink',
  component: AppLink,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof AppLink>;

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Inline link">
        <Text>
          Ask the agent in <AppLink to="/chat">Chat</AppLink>.
        </Text>
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="size">
        <Option name="inherit" use="Takes the size of the surrounding text. The default.">
          <AppLink to="/chat">Open chat</AppLink>
        </Option>
        <Option name="sm" use="Small text, for secondary links such as under a form.">
          <AppLink size="sm" to="/chat">
            Open chat
          </AppLink>
        </Option>
        <Option name="md" use="Regular body text size.">
          <AppLink size="md" to="/chat">
            Open chat
          </AppLink>
        </Option>
      </OptionTable>
    </Stack>
  ),
};
