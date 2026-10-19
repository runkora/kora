'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Agent response turn: branded byline + thinking block + interleaved content elements.
 */

import { memo } from 'react';
import { Icon, Stack, Text } from '@runkora/hades';
import type { AgentTurn, AgentElement, ToolCall } from '../_types';
import { useRunContext } from '../_context/run-context';
import { ThinkingBlock } from './thinking-block';
import { AgentTextBlock } from './agent-text-block';
import { ActivityGroup } from './activity-group';
import { ApprovalPrompt } from './approval-prompt';

type Group =
  | { kind: 'tools'; calls: ToolCall[] }
  | { kind: 'prose'; items: Exclude<AgentElement, { kind: 'tool' }>[] };

/**
 * Splits a turn into prose and activity groups in their original order. A
 * subagent's calls go to childrenOf under their Agent call instead, so they
 * neither break a group nor show as the parent's own calls.
 */
export function groupElements(elements: AgentElement[]): {
  groups: Group[];
  childrenOf: Map<string, ToolCall[]>;
} {
  const ids = new Set(elements.flatMap((el) => (el.kind === 'tool' ? [el.tool.id] : [])));
  const childrenOf = new Map<string, ToolCall[]>();
  const groups: Group[] = [];
  for (const el of elements) {
    const last = groups[groups.length - 1];
    if (el.kind === 'tool') {
      const parent = el.tool.parentId;
      if (parent && ids.has(parent)) {
        childrenOf.set(parent, [...(childrenOf.get(parent) ?? []), el.tool]);
      } else if (last?.kind === 'tools') {
        last.calls.push(el.tool);
      } else {
        groups.push({ kind: 'tools', calls: [el.tool] });
      }
    } else if (last?.kind === 'prose') {
      last.items.push(el);
    } else {
      groups.push({ kind: 'prose', items: [el] });
    }
  }
  return { groups, childrenOf };
}

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
  const { agentName, model, showAllCalls, slug } = useRunContext();

  const reasoningActive = isGenerating && turn.elements.length === 0;
  const { groups, childrenOf } = groupElements(turn.elements);

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
        {model && (
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
        )}
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
          const isLastGroup = gi === groups.length - 1;
          if (group.kind === 'tools') {
            return (
              <Stack key={gi} style={{ maxWidth: '90ch', minWidth: 0 }}>
                <ActivityGroup
                  calls={group.calls}
                  childrenOf={childrenOf}
                  live={isGenerating && isLastGroup}
                  showAll={showAllCalls}
                  slug={slug}
                />
              </Stack>
            );
          }
          return (
            <Stack key={gi} style={{ maxWidth: '90ch', gap: 8 }}>
              {group.items.map((el, i) => {
                if (el.kind === 'text') {
                  const isLiveText = isGenerating && isLastGroup && i === group.items.length - 1;
                  return <AgentTextBlock key={i} content={el.content} isStreaming={isLiveText} />;
                }
                if (el.kind === 'stopped')
                  return (
                    <Stack
                      key={`stopped-${i}`}
                      direction="horizontal"
                      align="center"
                      data-stop-marker=""
                      style={{ gap: 6 }}
                    >
                      <Icon name="circle-x" size="xs" />
                      <Text size="supporting" color="muted">
                        {el.byGuardrail ? 'Stopped by Orbit' : 'Stopped by you'}
                      </Text>
                    </Stack>
                  );
                if (el.kind === 'notice')
                  return (
                    <Stack
                      key={`notice-${i}`}
                      direction="horizontal"
                      align="center"
                      data-notice=""
                      style={{ gap: 6 }}
                    >
                      <Icon name={el.tone === 'error' ? 'circle-alert' : 'info'} size="xs" />
                      <Text
                        size="supporting"
                        color={el.tone === 'error' ? undefined : 'muted'}
                        style={
                          el.tone === 'error' ? { color: 'var(--color-status-error)' } : undefined
                        }
                      >
                        {el.message}
                      </Text>
                    </Stack>
                  );
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
      </Stack>
    </Stack>
  );
});
