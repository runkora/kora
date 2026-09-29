'use client';

/**
 * @tier 3 — run-chat-scoped.
 * The agent's tool calls between two pieces of prose. Quiet by default: one
 * summary line, or a live line for the call in flight. Failures and policy
 * denials always show. A subagent's calls nest under its Agent call, and
 * "Show every call" lists every call flat, for audit.
 */

import { memo, useState } from 'react';
import Link from 'next/link';
import { APP_ROUTES } from '@orbit/lib/routing';
import { Badge, Button, Icon, Stack, StatusDot, Text } from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import type { ToolCall } from '../_types';
import {
  callDuration,
  describeTool,
  formatDuration,
  isFailure,
  isSandboxProvision,
  outcome,
  policyDenial,
  spanDuration,
  summarize,
  type ToolKind,
} from '../_lib/tool-activity';
import { useNow } from '../_hooks/use-now';
import { ToolResultView } from './tool-result-view';

const KIND_ICONS: Record<ToolKind, IconName> = {
  command: 'square-terminal',
  read: 'file-text',
  write: 'edit',
  search: 'search',
  web: 'globe',
  agent: 'bot',
  mcp: 'network',
  platform: 'box',
  other: 'wrench',
};

type ChildrenOf = Map<string, ToolCall[]>;

function Elapsed({ tool }: { tool: ToolCall }) {
  const now = useNow(tool.status === 'running');
  const ms =
    tool.status === 'running'
      ? Math.max((tool.progressSeconds ?? 0) * 1000, now - (tool.receivedAt ?? now))
      : callDuration(tool);
  if (ms === null) return null;
  return (
    <Text as="span" mono style={{ fontSize: 11, color: 'var(--color-subtle)' }}>
      {formatDuration(Math.max(0, ms))}
    </Text>
  );
}

function OutcomeChip({ tool, childCount }: { tool: ToolCall; childCount: number }) {
  const denial = policyDenial(tool);
  if (denial) {
    return (
      <Badge variant="error" size="sm" data-policy-denial="">
        <Icon name="shield-off" size="2xs" />
        Blocked by policy{denial.blocked ? `: ${denial.blocked}` : ''}
      </Badge>
    );
  }
  if (tool.status === 'running') return <StatusDot status="pending" size="sm" glow />;
  const label =
    childCount > 0 ? `${childCount} ${childCount === 1 ? 'call' : 'calls'}` : outcome(tool);
  const failed = isFailure(tool);
  return (
    <Badge variant={failed ? 'error' : tool.status === 'stopped' ? 'muted' : 'dim'} size="sm" mono>
      {tool.status === 'stopped' ? 'stopped' : label}
    </Badge>
  );
}

/** The first line of a failure, so an error reads without expanding it. */
function firstErrorLine(tool: ToolCall): string {
  return (
    tool.result
      .replace(/<\/?tool_use_error>/g, '')
      .split('\n')
      .map((l) => l.trim())
      .find(Boolean) ?? ''
  );
}

export const ToolCallRow = memo(function ToolCallRow({
  tool,
  childrenOf,
  showAll,
  slug,
}: {
  tool: ToolCall;
  childrenOf: ChildrenOf;
  showAll: boolean;
  slug: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const d = describeTool(tool.name, tool.input);
  const children = childrenOf.get(tool.id) ?? [];
  const failed = isFailure(tool);
  const errorLine = failed ? firstErrorLine(tool) : '';

  return (
    <Stack data-tool-call="" data-tool-status={tool.status} style={{ gap: 0, minWidth: 0 }}>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        style={{
          justifyContent: 'flex-start',
          gap: 8,
          width: '100%',
          minWidth: 0,
          height: 'auto',
          padding: '3px 6px',
        }}
      >
        <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size="2xs" />
        <Icon name={KIND_ICONS[d.kind]} size="xs" />
        <Text as="span" style={{ fontSize: 13, color: 'var(--color-foreground)', flexShrink: 0 }}>
          {d.label}
        </Text>
        {d.target && (
          <Text
            as="span"
            mono
            style={{
              fontSize: 12,
              color: 'var(--color-muted)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              minWidth: 0,
              flex: '0 1 auto',
            }}
          >
            {d.target}
          </Text>
        )}
        <Text as="span" aria-hidden style={{ flex: 1 }} />
        <OutcomeChip tool={tool} childCount={children.length} />
        <Elapsed tool={tool} />
      </Button>
      {failed && !expanded && (errorLine || policyDenial(tool)) && (
        <Stack
          direction="horizontal"
          align="center"
          style={{ gap: 8, padding: '0 0 4px 32px', flexWrap: 'wrap' }}
        >
          {errorLine && (
            <Text
              as="span"
              mono
              data-error-line=""
              style={{ fontSize: 11, color: 'var(--color-status-error)', overflowWrap: 'anywhere' }}
            >
              {errorLine}
            </Text>
          )}
          {policyDenial(tool) && (
            <Text as="span" size="supporting">
              <Link href={APP_ROUTES.agents.detail(slug)}>Review this agent&apos;s access</Link>
            </Text>
          )}
        </Stack>
      )}
      {expanded && <ToolResultView tool={tool} />}
      {children.length > 0 && (
        <Stack
          data-subagent-calls=""
          style={{
            marginLeft: 14,
            paddingLeft: 10,
            borderLeft: '1px solid var(--color-border-subtle)',
          }}
        >
          <ActivityGroup
            calls={children}
            childrenOf={childrenOf}
            live={tool.status === 'running'}
            showAll={showAll}
            slug={slug}
          />
        </Stack>
      )}
    </Stack>
  );
});

/** A platform call shown as a status line rather than a tool call. */
function SandboxLine({ tool }: { tool: ToolCall }) {
  const now = useNow(tool.status === 'running');
  const ms = tool.status === 'running' ? now - (tool.receivedAt ?? now) : callDuration(tool);
  const text =
    tool.status === 'running'
      ? 'Starting sandbox…'
      : tool.status === 'error'
        ? 'Sandbox failed to start'
        : 'Sandbox ready';
  return (
    <Stack
      direction="horizontal"
      align="center"
      data-platform-status=""
      style={{ gap: 8, padding: '3px 6px' }}
    >
      <StatusDot
        status={
          tool.status === 'error' ? 'error' : tool.status === 'running' ? 'pending' : 'healthy'
        }
        size="sm"
      />
      <Text as="span" size="supporting" color={tool.status === 'error' ? undefined : 'muted'}>
        {text}
        {ms !== null ? ` · ${formatDuration(Math.max(0, ms))}` : ''}
      </Text>
    </Stack>
  );
}

function CallLine({
  tool,
  childrenOf,
  showAll,
  slug,
}: {
  tool: ToolCall;
  childrenOf: ChildrenOf;
  showAll: boolean;
  slug: string;
}) {
  return isSandboxProvision(tool) ? (
    <SandboxLine tool={tool} />
  ) : (
    <ToolCallRow tool={tool} childrenOf={childrenOf} showAll={showAll} slug={slug} />
  );
}

export function ActivityGroup({
  calls,
  childrenOf,
  live,
  showAll,
  slug,
}: {
  calls: ToolCall[];
  childrenOf: ChildrenOf;
  live: boolean;
  showAll: boolean;
  slug: string;
}) {
  const [open, setOpen] = useState(false);
  const platform = calls.filter(isSandboxProvision);
  const tools = calls.filter((t) => !isSandboxProvision(t));
  const running = tools.filter((t) => t.status === 'running');
  const failures = tools.filter(isFailure);
  const quiet = tools.filter((t) => !isFailure(t));

  if (showAll || tools.length <= 1) {
    return (
      <Stack data-activity-group="" style={{ gap: 0, minWidth: 0 }}>
        {calls.map((t) => (
          <CallLine key={t.id} tool={t} childrenOf={childrenOf} showAll={showAll} slug={slug} />
        ))}
      </Stack>
    );
  }

  const current = live ? running.at(-1) : undefined;
  const earlier = quiet.filter((t) => t !== current);
  const duration = spanDuration(tools);
  return (
    <Stack data-activity-group="" style={{ gap: 0, minWidth: 0 }}>
      {platform.map((t) => (
        <SandboxLine key={t.id} tool={t} />
      ))}
      {earlier.length > 0 && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          data-activity-summary=""
          style={{
            justifyContent: 'flex-start',
            gap: 8,
            width: '100%',
            height: 'auto',
            padding: '3px 6px',
          }}
        >
          <Icon name={open ? 'chevron-down' : 'chevron-right'} size="2xs" />
          <Text as="span" size="supporting" color="muted">
            {summarize(earlier)}
            {!current && duration !== null ? ` · ${formatDuration(duration)}` : ''}
          </Text>
        </Button>
      )}
      {open && (
        <Stack style={{ paddingLeft: 12, gap: 0, minWidth: 0 }}>
          {earlier.map((t) => (
            <ToolCallRow
              key={t.id}
              tool={t}
              childrenOf={childrenOf}
              showAll={showAll}
              slug={slug}
            />
          ))}
        </Stack>
      )}
      {failures.map((t) => (
        <ToolCallRow key={t.id} tool={t} childrenOf={childrenOf} showAll={showAll} slug={slug} />
      ))}
      {current && (
        <ToolCallRow tool={current} childrenOf={childrenOf} showAll={showAll} slug={slug} />
      )}
    </Stack>
  );
}
