/**
 * Chat Primitives — InputSurface · TypingIndicator · StreamingCursor
 *
 * Three focused building blocks for the chat interface. These are distinct
 * from `ChatBubble` / `ChatBubbleActions`, which handle the conversational
 * message layout.
 *
 * - **InputSurface** — a rounded container with a `focus-within` accent
 *   border that activates whenever any descendant receives focus. Use this
 *   as the outer wrapper for the chat input area.
 *
 * - **TypingIndicator** — three animated bounce-dots shown while the AI is
 *   generating a response. Nest inside a `ChatBubble` with `direction="start"`.
 *
 * - **StreamingCursor** — a blinking inline cursor placed at the end of a
 *   partial streaming response. Nest directly after your message content
 *   inside the bubble.
 */

import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '../button/button';
import { ChatBubble } from './chat-bubble';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { InputSurface, TypingIndicator, StreamingCursor } from './chat';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta: Meta = {
  title: 'Terra/Atoms/Chat Primitives',
  parameters: {
    layout: 'padded',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
};

export default meta;
type Story = StoryObj;

// ─── Stories ──────────────────────────────────────────────────────────────────

/**
 * `InputSurface` wraps the chat textarea and toolbar. The border transitions
 * from `border-border` to `border-accent` as soon as any descendant is
 * focused — click inside the box below to see the transition.
 */
export const InputSurfaceStory: Story = {
  name: 'InputSurface',
  render: () => (
    <InputSurface className="max-w-lg">
      <textarea
        className="w-full resize-none bg-transparent px-4 py-3 text-sm text-foreground placeholder-muted outline-none"
        rows={3}
        placeholder="Message Orbit…"
      />
      <Stack
        direction="horizontal"
        align="center"
        justify="between"
        paddingInline="snug"
        paddingBottom="snug"
        gap="snug"
        asChild
      >
        <footer>
          <Text size="micro" color="subtle">
            Shift + Enter for new line
          </Text>
          <Button size="xs" shape="pill">
            <Icon name="send" size="xs" />
            Send
          </Button>
        </footer>
      </Stack>
    </InputSurface>
  ),
};

/**
 * `TypingIndicator` nests inside a `ChatBubble` to signal that the AI is
 * thinking. The three dots animate with staggered delays.
 */
export const TypingIndicatorStory: Story = {
  name: 'TypingIndicator',
  render: () => (
    <Stack gap="snug" className="w-[420px]">
      <ChatBubble direction="start">
        <TypingIndicator />
      </ChatBubble>
    </Stack>
  ),
};

/**
 * `StreamingCursor` is an inline-block span that pulses while a response
 * is streaming. Place it immediately after your message content inside the
 * bubble — it will appear at the end of the last rendered word.
 */
export const StreamingCursorStory: Story = {
  name: 'StreamingCursor',
  render: () => (
    <Stack gap="snug" className="w-[420px]">
      <ChatBubble direction="start">
        <Text as="span" size="supporting" preserveWhitespace>
          Evaluating the active policy bundle against the incoming credential request
        </Text>
        <StreamingCursor />
      </ChatBubble>
    </Stack>
  ),
};

/**
 * All three primitives in a realistic chat sequence: first the AI shows a
 * thinking indicator, then transitions to a streaming response.
 */
export const ChatSequence: Story = {
  name: 'Chat sequence (all three)',
  render: () => (
    <Stack gap="snug" className="w-[480px]">
      {/* User message */}
      <ChatBubble direction="end" variant="accent">
        <Text as="span" size="supporting">
          How is the policy engine performing right now?
        </Text>
      </ChatBubble>

      {/* AI thinking */}
      <ChatBubble direction="start">
        <TypingIndicator />
      </ChatBubble>

      {/* AI streaming */}
      <ChatBubble direction="start">
        <Text as="span" size="supporting" preserveWhitespace>
          The policy engine evaluated 2,341 decisions in the last hour with zero violations across
          all registered agents.
        </Text>
        <StreamingCursor />
      </ChatBubble>

      {/* Input surface */}
      <InputSurface>
        <div className="px-4 py-3 text-sm text-muted-foreground">Message Orbit…</div>
        <Stack
          direction="horizontal"
          align="center"
          justify="end"
          paddingInline="snug"
          paddingBottom="snug"
        >
          <Button size="xs" shape="pill" disabled>
            <Icon name="send" size="xs" />
            Send
          </Button>
        </Stack>
      </InputSurface>
    </Stack>
  ),
};
