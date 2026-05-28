'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Right-aligned message bubble for the human side of the conversation.
 */

import { Stack, Text, ChatBubble } from '@orbit/terra';
import type { HumanTurn } from '../_types';

export function HumanTurnItem({ turn }: { turn: HumanTurn }) {
  return (
    <Stack style={{ padding: '4px 20px' }}>
      <ChatBubble direction="end" variant="accent">
        <Text
          as="span"
          style={{
            fontSize: 14,
            lineHeight: 1.6,
            wordBreak: 'break-word',
            overflowWrap: 'break-word',
          }}
        >
          {turn.content}
        </Text>
      </ChatBubble>
    </Stack>
  );
}
