'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Agent response turn: branded byline + thinking block + interleaved content elements.
 */

import { memo } from 'react';
import { Stack, Text, OrbitalLoader } from '@orbit/terra';
import type { AgentTurn, AgentElement } from '../_types';
import { useRunContext } from '../_context/run-context';
import { ThinkingBlock } from './thinking-block';
import { AgentTextBlock } from './agent-text-block';
import { ToolRow } from './tool-row';
import { ApprovalPrompt } from './approval-prompt';

function OrbitalMark() {
  return (
    <svg width="13" height="13" viewBox="0 0 12 12" fill="none" aria-hidden>
      <circle cx="6" cy="6" r="2.5" fill="currentColor" />
      <circle
        cx="6"
        cy="6"
        r="5"
        stroke="currentColor"
        strokeWidth="1.2"
        fill="none"
        opacity=".45"
      />
    </svg>
  );
}

export const AgentTurnItem = memo(function AgentTurnItem({
  turn,
  isGenerating = false,
}: {
  turn: AgentTurn;
  isGenerating?: boolean;
}) {
  const { agentName, model } = useRunContext();

  const hasTools = turn.elements.some((el) => el.kind === 'tool');
  const hasContent = turn.elements.some((el) => el.kind !== 'tool');
  const reasoningActive = isGenerating && !hasTools && !hasContent;

  // Group consecutive tool elements so they share a fit-content wrapper while
  // preserving the original interleaved order of text and tool blocks.
  type Group =
    | { kind: 'tools'; items: Extract<AgentElement, { kind: 'tool' }>[] }
    | { kind: 'prose'; items: Exclude<AgentElement, { kind: 'tool' }>[] };

  const groups = turn.elements.reduce<Group[]>((acc, el) => {
    if (el.kind === 'tool') {
      const last = acc[acc.length - 1];
      if (last?.kind === 'tools') {
        last.items.push(el);
      } else {
        acc.push({ kind: 'tools', items: [el] });
      }
    } else {
      const last = acc[acc.length - 1];
      if (last?.kind === 'prose') {
        last.items.push(el as Exclude<AgentElement, { kind: 'tool' }>);
      } else {
        acc.push({ kind: 'prose', items: [el as Exclude<AgentElement, { kind: 'tool' }>] });
      }
    }
    return acc;
  }, []);

  return (
    <Stack style={{ padding: '16px 20px 16px' }}>
      {/* Byline */}
      <Stack direction="horizontal" align="center" style={{ gap: 8, marginBottom: 12 }}>
        <Stack
          align="center"
          justify="center"
          style={{
            width: 28,
            height: 28,
            flexShrink: 0,
            background: 'var(--color-surface-elevated)',
            borderRadius: 6,
            color: 'var(--color-accent)',
          }}
          aria-hidden
        >
          <OrbitalMark />
        </Stack>
        <Text as="span" style={{ fontWeight: 500, fontSize: 14, color: 'var(--color-foreground)' }}>
          {agentName}
        </Text>
        <Text
          as="span"
          style={{
            fontSize: 10,
            fontFamily: 'var(--font-mono)',
            color: 'var(--color-subtle)',
            border: '1px solid var(--color-border)',
            borderRadius: 3,
            padding: '0 5px',
            lineHeight: '16px',
          }}
        >
          {model}
        </Text>
        <Text
          as="span"
          style={{ fontSize: 12, color: 'var(--color-subtle)', fontFamily: 'var(--font-mono)' }}
        >
          · {turn.timestamp}
        </Text>
      </Stack>

      {/* Content — rendered in original element order */}
      <Stack style={{ gap: 12 }}>
        {turn.thinking && <ThinkingBlock thinking={turn.thinking} isActive={reasoningActive} />}

        {groups.map((group, gi) => {
          if (group.kind === 'tools') {
            return (
              <Stack key={gi} style={{ width: 'fit-content', gap: 4 }}>
                {group.items.map((el) => (
                  <ToolRow key={el.tool.id} tool={el.tool} isCommitted={!isGenerating} />
                ))}
              </Stack>
            );
          }
          const isLastGroup = gi === groups.length - 1;
          return (
            <Stack key={gi} style={{ maxWidth: '90ch', gap: 8 }}>
              {group.items.map((el, i) => {
                if (el.kind === 'text') {
                  const isLiveText = isGenerating && isLastGroup && i === group.items.length - 1;
                  return <AgentTextBlock key={i} content={el.content} isStreaming={isLiveText} />;
                }
                if (el.kind === 'approval')
                  return (
                    <ApprovalPrompt
                      key={el.id}
                      id={el.id}
                      title={el.title}
                      description={el.description}
                      command={el.command}
                    />
                  );
                return null;
              })}
            </Stack>
          );
        })}

        {/* Orbital loader — shown while streaming, below any existing content */}
        {isGenerating && (
          <Stack style={{ padding: '4px 0 2px' }}>
            <OrbitalLoader size="md" />
          </Stack>
        )}
      </Stack>
    </Stack>
  );
});
