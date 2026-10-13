import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Textarea } from '../textarea/textarea';
import { ChatBubble } from './chat-bubble';
import { InputSurface, StreamingCursor, TypingIndicator } from './chat';

/**
 * Small pieces for a chat screen: the box around the message input, the dots
 * shown while an answer is being prepared, and the cursor shown while it streams in.
 */
const meta: Meta<typeof InputSurface> = {
  title: 'Hades/Atoms/Chat',
  component: InputSurface,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof InputSurface>;

function ComposerSample() {
  return (
    <InputSurface>
      <Textarea variant="bare" rows={2} aria-label="Message" placeholder="Ask the agent anything" />
      <Stack direction="horizontal" justify="end" paddingInline="snug" paddingBlock="tight">
        <Button size="xs" shape="pill">
          <Icon name="send" size="2xs" />
          Send
        </Button>
      </Stack>
    </InputSurface>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Message input">
        <ComposerSample />
      </Example>
      <Example title="Waiting for an answer">
        <ChatBubble direction="start">
          <TypingIndicator />
        </ChatBubble>
      </Example>
      <Example title="Answer arriving">
        <ChatBubble direction="start">
          <Text as="span">Checking which agent can run this job</Text>
          <StreamingCursor />
        </ChatBubble>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Pieces" note="None of these take props. Each one is used as it is.">
        <Option
          name="InputSurface"
          use="A rounded box for the message input. Its border turns to the accent colour when anything inside has focus."
        >
          <ComposerSample />
        </Option>
        <Option
          name="TypingIndicator"
          use="Three bouncing dots. Show them before the first words of an answer arrive."
        >
          <TypingIndicator />
        </Option>
        <Option
          name="StreamingCursor"
          use="A blinking bar. Put it right after the text while the answer is still coming in."
        >
          <Text as="span">Here is the first part of the answer</Text>
          <StreamingCursor />
        </Option>
      </OptionTable>
    </div>
  ),
};
