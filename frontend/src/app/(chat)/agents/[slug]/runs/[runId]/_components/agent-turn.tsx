'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Agent response turn: branded byline + thinking block + interleaved content elements.
 */

import { Stack, Text } from '@orbit/terra';
import type { AgentTurn } from '../_types';
import { ThinkingBlock } from './thinking-block';
import { AgentTextBlock } from './agent-text-block';
import { ToolRow } from './tool-row';
import { ApprovalPrompt } from './approval-prompt';

function OrbitalMark() {
  return (
    <svg width="9" height="9" viewBox="0 0 12 12" fill="none" aria-hidden>
      <circle cx="6" cy="6" r="2.5" fill="currentColor" />
      <circle
        cx="6"
        cy="6"
        r="5"
        stroke="currentColor"
        strokeWidth="1.2"
        fill="none"
        opacity=".5"
      />
    </svg>
  );
}

export function AgentTurnItem({ turn }: { turn: AgentTurn }) {
  return (
    <Stack style={{ padding: '12px 20px 4px' }}>
      {/* Byline */}
      <Stack direction="horizontal" align="center" style={{ gap: 8, marginBottom: 10 }}>
        <Stack
          align="center"
          justify="center"
          style={{
            width: 18,
            height: 18,
            background: 'var(--chat-acc-dim)',
            border: '1.5px solid var(--chat-acc-bdr)',
            borderRadius: 3,
            color: 'var(--chat-acc)',
            flexShrink: 0,
          }}
          aria-hidden
        >
          <OrbitalMark />
        </Stack>
        <Text as="span" style={{ fontSize: 12, color: 'var(--chat-muted)', fontWeight: 500 }}>
          Orbit AI
        </Text>
        <Text
          as="span"
          style={{
            fontSize: 11,
            color: 'var(--chat-subtle)',
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          }}
        >
          {turn.timestamp}
        </Text>
      </Stack>

      {/* Content area — indented 26px to align with byline text */}
      <Stack style={{ paddingLeft: 26 }}>
        {/* Thinking block (before elements, if present) */}
        {turn.thinking && <ThinkingBlock thinking={turn.thinking} />}

        {/* Element flow */}
        {turn.elements.map((el, i) => {
          if (el.kind === 'text') {
            return <AgentTextBlock key={i} content={el.content} />;
          }
          if (el.kind === 'tool') {
            return <ToolRow key={el.tool.id} tool={el.tool} />;
          }
          if (el.kind === 'approval') {
            return (
              <ApprovalPrompt
                key={el.id}
                id={el.id}
                title={el.title}
                description={el.description}
                command={el.command}
              />
            );
          }
          return null;
        })}
      </Stack>
    </Stack>
  );
}
