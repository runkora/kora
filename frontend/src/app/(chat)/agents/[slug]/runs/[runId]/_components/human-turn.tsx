'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Right-aligned message bubble for the human side of the conversation, with
 * the delivery state of a message sent while the agent was working. A message
 * a trigger sent is labelled with the trigger's name.
 */

import { ChatBubble, Icon, Stack, Text } from '@runkora/hades';
import type { HumanTurn } from '../_types';

const DELIVERY_TEXT = {
  queued: 'Queued: the agent reads it at its next step',
  delivered: 'Delivered',
} as const;

export function HumanTurnItem({ turn }: { turn: HumanTurn }) {
  return (
    <Stack style={{ padding: '4px 20px', gap: 4 }} data-delivery={turn.delivery}>
      {turn.fromTrigger !== undefined && (
        <Stack direction="horizontal" align="center" gap="tight" justify="end">
          <Icon name="zap" size="2xs" color="subtle" label="" />
          <Text size="supporting" color="muted">
            From trigger {turn.fromTrigger}
          </Text>
        </Stack>
      )}
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
      {turn.delivery && (
        <Text
          size="supporting"
          color={turn.delivery === 'failed' ? undefined : 'muted'}
          role={turn.delivery === 'failed' ? 'alert' : undefined}
          style={{
            alignSelf: 'flex-end',
            color: turn.delivery === 'failed' ? 'var(--color-status-error)' : undefined,
          }}
        >
          {turn.delivery === 'failed'
            ? `Not sent. ${turn.error ?? ''}`
            : DELIVERY_TEXT[turn.delivery]}
        </Text>
      )}
    </Stack>
  );
}
