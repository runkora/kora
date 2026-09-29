import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { ChatBubble, ChatBubbleActions } from './chat-bubble';

/**
 * One chat message. It sets the side, the corner shape and the colour, and
 * can show a row of actions when the pointer is over it.
 */
const meta: Meta<typeof ChatBubble> = {
  title: 'Terra/Atoms/ChatBubble',
  component: ChatBubble,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof ChatBubble>;

function CopyActions() {
  return (
    <ChatBubbleActions>
      <Button variant="ghost" size="icon-sm" aria-label="Copy message">
        <Icon name="copy" size="2xs" />
      </Button>
      <Button variant="ghost" size="icon-sm" aria-label="Good answer">
        <Icon name="thumbs-up" size="2xs" />
      </Button>
    </ChatBubbleActions>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="A conversation">
        <Stack gap="comfortable">
          <ChatBubble direction="end" variant="accent">
            <Text as="span">Which agent should run this?</Text>
          </ChatBubble>
          <ChatBubble direction="start">
            <Text as="span">The billing agent. It already has access to the invoices.</Text>
            <CopyActions />
          </ChatBubble>
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option
          name='direction="end"'
          use="Sits on the right. Use it for the person's own messages."
        >
          <ChatBubble direction="end">
            <Text as="span">Run it again.</Text>
          </ChatBubble>
        </Option>
        <Option name='direction="start"' use="Sits on the left. Use it for replies.">
          <ChatBubble direction="start">
            <Text as="span">Started a new run.</Text>
          </ChatBubble>
        </Option>
        <Option name='variant="surface"' use="Neutral background with a soft border. The default.">
          <ChatBubble direction="start" variant="surface">
            <Text as="span">Neutral background.</Text>
          </ChatBubble>
        </Option>
        <Option name='variant="accent"' use="Tinted with the accent colour.">
          <ChatBubble direction="start" variant="accent">
            <Text as="span">Accent background.</Text>
          </ChatBubble>
        </Option>
        <Option
          name="ChatBubbleActions"
          use="Buttons under the message. They appear when the pointer is over the bubble, so hover this one."
        >
          <ChatBubble direction="start">
            <Text as="span">Hover to see the actions.</Text>
            <CopyActions />
          </ChatBubble>
        </Option>
      </OptionTable>
    </div>
  ),
};
