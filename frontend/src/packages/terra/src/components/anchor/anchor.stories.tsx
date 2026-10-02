import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Anchor } from './anchor';

/**
 * Anchor
 *
 * The link style for Terra: accent colour, underline on hover, and a focus
 * ring. In the app, use AppLink for pages and ExternalLink for other sites.
 */
const meta: Meta<typeof Anchor> = {
  title: 'Terra/Atoms/Anchor',
  component: Anchor,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Anchor>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Standalone links">
        <Stack gap="snug">
          <Anchor href="#docs">Read the documentation</Anchor>
          <Anchor href="#agents">View all agents</Anchor>
        </Stack>
      </Example>
      <Example title="Next to a form field">
        <Stack direction="horizontal" justify="between" align="center">
          <Text size="supporting">Password</Text>
          <Anchor href="#reset" size="sm">
            Forgot password?
          </Anchor>
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='size="inherit"' use="Takes the size of the text around it. The default.">
          <Text size="supporting">
            <Anchor href="#one">Follows the parent size</Anchor>
          </Text>
        </Option>
        <Option name='size="md"' use="Standard small text size, for a link on its own.">
          <Anchor href="#two" size="md">
            Read the documentation
          </Anchor>
        </Option>
        <Option name='size="sm"' use="Extra small, for side links such as Forgot password.">
          <Anchor href="#three" size="sm">
            Forgot password?
          </Anchor>
        </Option>
        <Option name="asChild" use="Style your own link element, such as a router link.">
          <Anchor asChild size="md">
            <a href="#four">Go to dashboard</a>
          </Anchor>
        </Option>
      </OptionTable>
    </div>
  ),
};
