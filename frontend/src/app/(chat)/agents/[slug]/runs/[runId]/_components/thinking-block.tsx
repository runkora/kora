'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Collapsible extended-thinking section with orbital loader toggle.
 */

import { useState } from 'react';
import { OrbitalLoader, Stack, Text, Button } from '@orbit/terra';
import type { ThinkingBlock as ThinkingBlockType } from '../_types';

export function ThinkingBlock({ thinking }: { thinking: ThinkingBlockType }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <Stack
      style={{
        margin: '4px 0 10px',
        borderLeft: '2px solid var(--chat-bds)',
        paddingLeft: 12,
      }}
    >
      {/* Toggle row */}
      <Button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          cursor: 'pointer',
          userSelect: 'none',
          fontSize: 11.5,
          color: 'var(--chat-subtle)',
          padding: '2px 0',
          background: 'none',
          border: 'none',
          width: '100%',
          textAlign: 'left',
        }}
      >
        <Text as="span" style={{ opacity: 0.7, flexShrink: 0 }}>
          <OrbitalLoader size="sm" />
        </Text>
        <Text as="span">Thought for {thinking.durationSeconds} seconds</Text>
        <Text
          as="span"
          style={{
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            fontSize: 10.5,
            opacity: 0.35,
            marginLeft: 6,
          }}
        >
          {thinking.tokenCount.toLocaleString()} tokens
        </Text>
        <Text
          as="span"
          style={{
            fontSize: 9,
            marginLeft: 'auto',
            transition: 'transform 0.2s',
            transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)',
          }}
          aria-hidden
        >
          ▶
        </Text>
      </Button>

      {/* Body (conditional) */}
      {expanded && (
        <Stack
          style={{
            marginTop: 6,
            fontSize: 12,
            color: 'var(--chat-subtle)',
            fontStyle: 'italic',
            lineHeight: 1.65,
          }}
        >
          {thinking.content}
        </Stack>
      )}
    </Stack>
  );
}
