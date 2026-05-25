'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Right-aligned message bubble for the human side of the conversation.
 */

import { Stack } from '@orbit/terra';
import type { HumanTurn } from '../_types';

export function HumanTurnItem({ turn }: { turn: HumanTurn }) {
  return (
    <Stack direction="horizontal" justify="end" style={{ padding: '4px 20px' }}>
      <Stack
        style={{
          maxWidth: '68%',
          background: 'var(--chat-acc-dim)',
          border: '1px solid var(--chat-acc-bdr)',
          borderRadius: '10px 10px 3px 10px',
          padding: '9px 14px',
          color: 'var(--chat-fg)',
          lineHeight: 1.55,
        }}
      >
        {turn.content}
      </Stack>
    </Stack>
  );
}
