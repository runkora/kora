'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Scrollable container that renders the full conversation turn list.
 */

import { useEffect, useRef } from 'react';
import { Stack, Text } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';
import { HumanTurnItem } from './human-turn';
import { AgentTurnItem } from './agent-turn';
import type { ErrorTurn } from '../_types';

function ErrorTurnItem({ turn }: { turn: ErrorTurn }) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      style={{
        gap: 8,
        margin: '4px 20px',
        padding: '10px 14px',
        borderRadius: 8,
        background: 'color-mix(in srgb, var(--color-destructive) 12%, transparent)',
        border: '1px solid color-mix(in srgb, var(--color-destructive) 30%, transparent)',
      }}
    >
      <Text style={{ fontSize: 12, color: 'var(--color-destructive)', lineHeight: 1.5, flex: 1 }}>
        {turn.message}
      </Text>
      <Text as="span" style={{ fontSize: 10, color: 'var(--chat-bds)', flexShrink: 0 }}>
        {turn.timestamp}
      </Text>
    </Stack>
  );
}

export function ChatThread() {
  const { turns } = useRunContext();
  const containerRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom on initial mount
  useEffect(() => {
    const el = containerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, []);

  // Auto-scroll when turns change, but only if already near bottom
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 100;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [turns]);

  return (
    <Stack
      ref={containerRef}
      style={{
        flex: 1,
        overflowY: 'auto',
        padding: '20px 0 12px',
        scrollBehavior: 'smooth',
      }}
    >
      {/* Custom scrollbar */}
      <style>{`
        .chat-thread-scroll::-webkit-scrollbar { width: 5px; }
        .chat-thread-scroll::-webkit-scrollbar-thumb {
          background: var(--chat-surf3); border-radius: 3px;
        }
      `}</style>

      {/* Session divider */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 10,
          padding: '8px 20px 6px',
          fontSize: 10.5,
          textTransform: 'uppercase',
          letterSpacing: '0.07em',
          color: 'var(--chat-bds)',
        }}
      >
        Session started · Today · 09:41
        <Text
          as="span"
          style={{
            flex: 1,
            height: 1,
            background: 'var(--chat-bds)',
            opacity: 0.4,
          }}
        />
      </Stack>

      {/* Turn list */}
      {turns.map((turn) => {
        if (turn.type === 'human') return <HumanTurnItem key={turn.id} turn={turn} />;
        if (turn.type === 'error') return <ErrorTurnItem key={turn.id} turn={turn} />;
        return <AgentTurnItem key={turn.id} turn={turn} />;
      })}
    </Stack>
  );
}
