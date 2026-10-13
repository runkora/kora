'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Collapsible thinking section with flashing accent dot + timeline line.
 * isActive=true: dot pulses via data-reasoning-pulse, label shows "REASONING · …"
 * isActive=false: static dot, label shows "REASONING · Xs"
 */

import { useState } from 'react';
import { Stack, Text } from '@clear-route/hades';
import type { ThinkingBlock as ThinkingBlockType } from '../_types';

function ChevronRightIcon({ open }: { open: boolean }) {
  return (
    <svg
      width="11"
      height="11"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      aria-hidden
      style={{
        transform: open ? 'rotate(90deg)' : 'none',
        transition: 'transform 0.18s',
        flexShrink: 0,
      }}
    >
      <polyline points="9 18 15 12 9 6" />
    </svg>
  );
}

export function ThinkingBlock({
  thinking,
  isActive = false,
}: {
  thinking: ThinkingBlockType;
  isActive?: boolean;
}) {
  const [expanded, setExpanded] = useState(false);
  const canExpand = !isActive && !!thinking.content;
  const showLine = expanded || isActive;
  const duration = thinking.durationSeconds > 0 ? `${thinking.durationSeconds}s` : null;

  return (
    <Stack style={{ marginBottom: 8, position: 'relative', gap: 0 }}>
      {/* Vertical timeline line — runs from dot through expanded content */}
      {showLine && (
        <Stack
          style={{
            position: 'absolute',
            left: 4,
            top: 16,
            bottom: 0,
            width: 1,
            background: 'linear-gradient(to bottom, var(--color-accent) 50%, transparent)',
            opacity: 0.35,
            pointerEvents: 'none',
          }}
        />
      )}

      {/* Header row */}
      <Stack
        direction="horizontal"
        align="center"
        style={{ gap: 8, padding: '3px 0', cursor: canExpand ? 'pointer' : 'default' }}
        onClick={canExpand ? () => setExpanded((v) => !v) : undefined}
      >
        {/* Dot — pulses while active via RUN_CSS keyframe */}
        <Stack
          data-reasoning-pulse={isActive ? '' : undefined}
          style={{
            width: 9,
            height: 9,
            borderRadius: '50%',
            background: 'var(--color-accent)',
            flexShrink: 0,
          }}
        />
        <Text
          as="span"
          style={{
            fontSize: 12,
            fontFamily: 'var(--font-mono)',
            letterSpacing: '0.06em',
            color: 'var(--color-subtle)',
            fontWeight: 500,
          }}
        >
          REASONING
          {duration ? ` · ${duration.toUpperCase()}` : isActive ? ' · …' : ''}
        </Text>
        {canExpand && (
          <Text
            as="span"
            style={{ color: 'var(--color-subtle)', display: 'flex', alignItems: 'center' }}
          >
            <ChevronRightIcon open={expanded} />
          </Text>
        )}
      </Stack>

      {/* Expanded content — height-capped with gradient fade */}
      {expanded && thinking.content && (
        <Stack
          style={{
            paddingLeft: 20,
            paddingTop: 8,
            position: 'relative',
            maxHeight: 400,
            overflow: 'hidden',
            gap: 0,
          }}
        >
          <Text
            style={{
              fontSize: 14,
              color: 'var(--color-muted)',
              fontStyle: 'italic',
              lineHeight: 1.7,
              paddingBottom: 16,
            }}
          >
            {thinking.content}
          </Text>
          <Stack
            style={{
              position: 'absolute',
              bottom: 0,
              left: 0,
              right: 0,
              height: 64,
              background:
                'linear-gradient(to bottom, transparent, color-mix(in oklch, var(--color-background) 30%, transparent))',
              pointerEvents: 'none',
            }}
          />
        </Stack>
      )}
    </Stack>
  );
}
