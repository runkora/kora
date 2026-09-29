/**
 * ChatBubble
 *
 * A compound component pair for rendering conversational message threads.
 * `ChatBubble` owns the bubble shape, rounding, and surface variant.
 * `ChatBubbleActions` is an optional overlay that reveals on `group-hover`.
 *
 * Rules of thumb:
 *   • `direction="end"`   — viewer's own messages (right-aligned, accent surface)
 *   • `direction="start"` — incoming / AI messages (left-aligned, default surface)
 *   • Nest `ChatBubbleActions` as the last child for a hover-reveal action row.
 *   • `className` is intentionally not exposed on either component.
 */

import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Icon } from '../icon/icon';
import { ChatBubble, ChatBubbleActions } from './chat-bubble';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta: Meta<typeof ChatBubble> = {
  title: 'Terra/Atoms/ChatBubble',
  component: ChatBubble,
  parameters: {
    layout: 'padded',
  },
  tags: ['autodocs'],
  argTypes: {
    direction: {
      control: 'radio',
      options: ['start', 'end'],
    },
    variant: {
      control: 'radio',
      options: ['surface', 'accent'],
    },
    className: { table: { disable: true } },
  },
};

export default meta;
type Story = StoryObj<typeof ChatBubble>;

// ─── Stories ──────────────────────────────────────────────────────────────────

/**
 * Default interactive story. Use the Controls panel to switch direction
 * and variant to explore the component in isolation.
 */
export const Default: Story = {
  args: {
    direction: 'start',
    variant: 'surface',
  },
  render: (args) => (
    <div className="w-[420px]">
      <ChatBubble {...args}>
        <Text as="span" size="supporting" preserveWhitespace>
          Hello! How can I help you today?
        </Text>
      </ChatBubble>
    </div>
  ),
};

/**
 * The two directions and their default variants side-by-side.
 * `start` represents an inbound/AI message; `end` represents the user's reply.
 */
export const Directions: Story = {
  render: () => (
    <Stack direction="vertical" gap="comfortable" className="w-[420px]">
      <ChatBubble direction="start" variant="surface">
        <Text as="span" size="supporting" preserveWhitespace>
          I have analysed the last 24 hours of agent activity. Three tools returned non-200
          responses.
        </Text>
      </ChatBubble>
      <ChatBubble direction="end" variant="surface">
        <Text as="span" size="supporting" preserveWhitespace>
          Which tools failed?
        </Text>
      </ChatBubble>
    </Stack>
  ),
};

/**
 * The two surface variants for `direction="start"` (inbound).
 * `surface` is the default; `accent` draws attention to a highlighted reply.
 */
export const Variants: Story = {
  render: () => (
    <Stack direction="vertical" gap="comfortable" className="w-[420px]">
      <ChatBubble direction="start" variant="surface">
        <Text as="span" size="supporting">
          Surface variant — default for AI messages.
        </Text>
      </ChatBubble>
      <ChatBubble direction="start" variant="accent">
        <Text as="span" size="supporting">
          Accent variant — use to highlight a key response or notification.
        </Text>
      </ChatBubble>
    </Stack>
  ),
};

/**
 * `ChatBubbleActions` renders as an opacity-0 overlay that transitions to
 * opacity-100 when the parent bubble receives a hover. Compose action buttons
 * inside it — they never contribute to the bubble's layout when hidden.
 *
 * Hover over the bubble below to reveal the actions.
 */
export const WithActions: Story = {
  name: 'With Actions (hover to reveal)',
  render: () => (
    <Stack direction="vertical" gap="comfortable" className="w-[420px]">
      <ChatBubble direction="start" variant="surface">
        <Text as="span" size="supporting" preserveWhitespace>
          The policy engine evaluated 2,341 decisions in the last hour with zero violations across
          all registered agents.
        </Text>
        <ChatBubbleActions>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Copy message"
          >
            <Icon name="copy" size="xs" />
          </button>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Thumbs up"
          >
            <Icon name="thumbs-up" size="xs" />
          </button>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Thumbs down"
          >
            <Icon name="thumbs-down" size="xs" />
          </button>
        </ChatBubbleActions>
      </ChatBubble>

      <ChatBubble direction="end" variant="surface">
        <Text as="span" size="supporting">
          Great, can you show me the detailed breakdown?
        </Text>
        <ChatBubbleActions>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Copy message"
          >
            <Icon name="copy" size="xs" />
          </button>
        </ChatBubbleActions>
      </ChatBubble>
    </Stack>
  ),
};

/**
 * A realistic multi-turn conversation thread showing rounding, alignment,
 * spacing, and variant conventions all working together. Hover any bubble
 * to see the action overlay.
 */
export const ConversationThread: Story = {
  name: 'Conversation Thread',
  render: () => (
    <Stack direction="vertical" gap="snug" className="w-[480px]">
      {/* AI greeting */}
      <ChatBubble direction="start" variant="surface">
        <Text as="span" size="supporting" preserveWhitespace>
          Hello! I&apos;m Orbit AI. I have visibility into all your deployed agents, policies, and
          credentials. How can I help?
        </Text>
        <ChatBubbleActions>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Copy message"
          >
            <Icon name="copy" size="xs" />
          </button>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Thumbs up"
          >
            <Icon name="thumbs-up" size="xs" />
          </button>
        </ChatBubbleActions>
      </ChatBubble>

      {/* User question */}
      <ChatBubble direction="end" variant="surface">
        <Text as="span" size="supporting">
          How many agents are currently running?
        </Text>
        <ChatBubbleActions>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Copy message"
          >
            <Icon name="copy" size="xs" />
          </button>
        </ChatBubbleActions>
      </ChatBubble>

      {/* AI answer */}
      <ChatBubble direction="start" variant="surface">
        <Text as="span" size="supporting" preserveWhitespace>
          You have 12 agents running across 3 environments:
          {'\n'}
          {'\n'}• Production: 7 agents (2 degraded){'\n'}• Staging: 3 agents (all healthy){'\n'}•
          Development: 2 agents (all healthy)
        </Text>
        <ChatBubbleActions>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Copy message"
          >
            <Icon name="copy" size="xs" />
          </button>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Thumbs up"
          >
            <Icon name="thumbs-up" size="xs" />
          </button>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Thumbs down"
          >
            <Icon name="thumbs-down" size="xs" />
          </button>
        </ChatBubbleActions>
      </ChatBubble>

      {/* Highlighted AI message */}
      <ChatBubble direction="start" variant="accent">
        <Text as="span" size="supporting">
          The 2 degraded agents in production are awaiting credential renewal. Would you like me to
          initiate a renewal flow?
        </Text>
        <ChatBubbleActions>
          <button
            type="button"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-surface-overlay hover:text-foreground"
            aria-label="Copy message"
          >
            <Icon name="copy" size="xs" />
          </button>
        </ChatBubbleActions>
      </ChatBubble>
    </Stack>
  ),
};
