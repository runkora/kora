'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Scrollable container that renders the full conversation turn list.
 *
 * Auto-scroll behaviour:
 *   - Continuously follows new content via ResizeObserver while streaming.
 *   - Pauses when the user scrolls up (user intent to read back).
 *   - Resumes when the user scrolls back within 80 px of the bottom.
 *   - Shows a "↓ Latest" pill when paused mid-stream so the user can
 *     jump back with one click.
 */

import { useEffect, useRef, useState } from 'react';
import { Button, Icon, Stack, Text, OrbitalLoader } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';
import { HumanTurnItem } from './human-turn';
import { AgentTurnItem } from './agent-turn';
import type { ErrorTurn } from '../_types';

const AT_BOTTOM_THRESHOLD = 80; // px from bottom considered "at bottom"

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
        background: 'color-mix(in oklch, var(--color-status-error) 12%, transparent)',
        border: '1px solid color-mix(in oklch, var(--color-status-error) 30%, transparent)',
      }}
    >
      <Text style={{ fontSize: 14, color: 'var(--color-status-error)', lineHeight: 1.5, flex: 1 }}>
        {turn.message}
      </Text>
      <Text as="span" style={{ fontSize: 12, color: 'var(--color-border)', flexShrink: 0 }}>
        {turn.timestamp}
      </Text>
    </Stack>
  );
}

export function ChatThread() {
  const { turns, isStreaming, isPending, isBusy } = useRunContext();
  const containerRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  // true when user has scrolled up — suppresses programmatic scroll
  const lockedRef = useRef(false);
  const [scrolledUp, setScrolledUp] = useState(false);

  const startTime = turns.length > 0 ? turns[0].timestamp.slice(0, 5) : null;

  function scrollToBottom() {
    const el = containerRef.current;
    if (!el) return;
    lockedRef.current = false;
    setScrolledUp(false);
    el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
  }

  // Track user scroll intent: lock when scrolled up, unlock when back at bottom
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const onScroll = () => {
      const dist = el.scrollHeight - el.scrollTop - el.clientHeight;
      const up = dist > AT_BOTTOM_THRESHOLD;
      lockedRef.current = up;
      setScrolledUp(up);
    };
    el.addEventListener('scroll', onScroll, { passive: true });
    return () => el.removeEventListener('scroll', onScroll);
  }, []);

  // ResizeObserver on inner content: fires on every new streamed character /
  // element added, scrolls to bottom unless the user has locked it.
  useEffect(() => {
    const content = contentRef.current;
    const scroll = containerRef.current;
    if (!content || !scroll) return;
    const ro = new ResizeObserver(() => {
      if (!lockedRef.current) {
        scroll.scrollTop = scroll.scrollHeight;
      }
    });
    ro.observe(content);
    return () => ro.disconnect();
  }, []);

  // Jump to bottom on initial mount — direct DOM write, no setState
  useEffect(() => {
    const el = containerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, []);

  const showJumpButton = scrolledUp && isStreaming;

  return (
    // Outer wrapper: grows to fill available space, anchors the jump button
    <Stack grow style={{ position: 'relative', minHeight: 0 }}>
      <style>{`
        [data-chat-scroll]::-webkit-scrollbar { width: 5px; }
        [data-chat-scroll]::-webkit-scrollbar-thumb {
          background: var(--color-surface-overlay); border-radius: 3px;
        }
        [data-jump-btn] {
          opacity: 0;
          transform: translateY(6px);
          animation: jump-btn-in 0.15s ease forwards;
        }
        @keyframes jump-btn-in { to { opacity: 1; transform: translateY(0); } }
      `}</style>

      {/* Scrollable thread */}
      <Stack
        ref={containerRef}
        data-chat-scroll=""
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: '20px 0 12px',
        }}
      >
        {/* Inner content wrapper — ResizeObserver target */}
        <Stack ref={contentRef} style={{ gap: 0, width: '100%' }}>
          {/* Session divider */}
          <Stack direction="horizontal" align="center" style={{ gap: 10, padding: '8px 20px 6px' }}>
            <Text
              as="span"
              style={{
                flex: 1,
                height: '1px',
                background: 'var(--color-border-subtle)',
                opacity: 0.4,
              }}
            />
            <Text
              as="span"
              style={{
                fontSize: 12,
                color: 'var(--color-subtle)',
                fontFamily: 'var(--font-mono)',
                flexShrink: 0,
                letterSpacing: '0.05em',
              }}
            >
              {startTime ? `Session · ${startTime}` : 'Session'}
            </Text>
            <Text
              as="span"
              style={{
                flex: 1,
                height: '1px',
                background: 'var(--color-border-subtle)',
                opacity: 0.4,
              }}
            />
          </Stack>

          {/* Turn list */}
          {turns.map((turn, i) => {
            const isLast = i === turns.length - 1;
            if (turn.type === 'human') return <HumanTurnItem key={turn.id} turn={turn} />;
            if (turn.type === 'error') return <ErrorTurnItem key={turn.id} turn={turn} />;
            return <AgentTurnItem key={turn.id} turn={turn} isGenerating={isLast && isStreaming} />;
          })}

          {/* Pending/busy indicator: shown while waiting for the agent to start responding */}
          {(isPending || isBusy) && !isStreaming && (
            <Stack style={{ padding: '16px 20px' }}>
              <OrbitalLoader size="md" />
            </Stack>
          )}
        </Stack>
      </Stack>

      {/* Jump-to-bottom pill — only shown when user scrolled up during streaming */}
      {showJumpButton && (
        <Stack
          data-jump-btn=""
          style={{
            position: 'absolute',
            bottom: 12,
            left: '50%',
            transform: 'translateX(-50%)',
            zIndex: 10,
          }}
        >
          <Button
            variant="outline"
            size="sm"
            onClick={() => scrollToBottom()}
            style={{
              background: 'var(--color-surface-elevated)',
              boxShadow: '0 2px 8px rgb(0 0 0 / 0.25)',
              borderColor: 'var(--color-border)',
              gap: 6,
              paddingLeft: 12,
              paddingRight: 14,
              whiteSpace: 'nowrap',
            }}
          >
            <Icon name="arrow-down" size="2xs" />
            <Text as="span" style={{ fontSize: 12, fontWeight: 500 }}>
              Latest
            </Text>
          </Button>
        </Stack>
      )}
    </Stack>
  );
}
