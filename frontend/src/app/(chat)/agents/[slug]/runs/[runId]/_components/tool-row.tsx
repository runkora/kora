'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Tool call card: emoji badge + name + arg + status dot + expandable result + canvas pop-out.
 */

import { memo, useState } from 'react';
import { Stack, Text, Button, StatusDot, Icon } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';
import type { ToolCall, ToolIconName } from '../_types';

const TOOL_ICON_STYLES: Record<ToolIconName, { color: string; background: string }> = {
  'square-terminal': {
    color: 'var(--color-status-healthy)',
    background: 'color-mix(in oklch, var(--color-status-healthy) 14%, transparent)',
  },
  'file-text': {
    color: '#f472b6',
    background: 'color-mix(in oklch, #f472b6 14%, transparent)',
  },
  edit: {
    color: '#60a5fa',
    background: 'color-mix(in oklch, #60a5fa 14%, transparent)',
  },
  search: {
    color: '#a78bfa',
    background: 'color-mix(in oklch, #a78bfa 14%, transparent)',
  },
  database: {
    color: '#fbbf24',
    background: 'color-mix(in oklch, #fbbf24 14%, transparent)',
  },
  'external-link': {
    color: '#22d3ee',
    background: 'color-mix(in oklch, #22d3ee 14%, transparent)',
  },
  wrench: {
    color: 'var(--color-subtle)',
    background: 'var(--color-surface-overlay)',
  },
};

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

function PopoutIcon() {
  return (
    <svg
      width="10"
      height="10"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      aria-hidden
    >
      <polyline points="15 3 21 3 21 9" />
      <polyline points="9 21 3 21 3 15" />
      <line x1="21" y1="3" x2="14" y2="10" />
      <line x1="3" y1="21" x2="10" y2="14" />
    </svg>
  );
}

export const ToolRow = memo(function ToolRow({
  tool,
  isCommitted = false,
}: {
  tool: ToolCall;
  isCommitted?: boolean;
}) {
  const { canvasState, openCanvas } = useRunContext();
  const [expanded, setExpanded] = useState(false);

  // When a turn is committed but tool_result hasn't updated status (e.g. live race),
  // treat the tool as done so "running" never shows on a finished turn.
  const effectiveStatus = isCommitted && tool.status === 'running' ? 'done' : tool.status;
  const canExpand = effectiveStatus !== 'running' && !!tool.result;
  const dotStatus =
    effectiveStatus === 'running' ? 'pending' : effectiveStatus === 'done' ? 'healthy' : 'error';
  const isCanvasActive =
    tool.canvasViewId !== undefined && canvasState.activeTabId === tool.canvasViewId;

  function handlePopout(e: React.MouseEvent) {
    e.stopPropagation();
    if (tool.canvasViewId) openCanvas(tool.canvasViewId);
  }

  return (
    <Stack
      style={{
        border: '1px solid var(--color-border-subtle)',
        borderRadius: 8,
        background: 'var(--color-surface-elevated)',
        overflow: 'hidden',
      }}
    >
      {/* Header row */}
      <Stack
        direction="horizontal"
        align="center"
        style={{ padding: '8px 12px', gap: 10, minWidth: 0 }}
      >
        {/* Icon badge */}
        <Stack
          align="center"
          justify="center"
          style={{
            width: 28,
            height: 28,
            borderRadius: 6,
            flexShrink: 0,
            background: TOOL_ICON_STYLES[tool.icon].background,
            color: TOOL_ICON_STYLES[tool.icon].color,
          }}
        >
          <Icon name={tool.icon} size="xs" />
        </Stack>

        {/* Name + arg — single truncated mono line */}
        <Text
          as="span"
          style={{
            flex: 1,
            minWidth: 0,
            fontFamily: 'var(--font-mono)',
            lineHeight: 1,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          <Text
            as="span"
            style={{
              fontSize: 12,
              lineHeight: 1,
              fontWeight: 500,
              color: 'var(--color-foreground)',
            }}
          >
            {tool.name}
          </Text>
          {tool.arg && (
            <>
              {' '}
              <Text as="span" style={{ fontSize: 12, lineHeight: 1, color: 'var(--color-muted)' }}>
                {tool.arg}
              </Text>
            </>
          )}
        </Text>

        {/* Right: status dot + status text + expand + canvas pop-out */}
        <Stack
          direction="horizontal"
          align="center"
          style={{ gap: 8, flexShrink: 0, marginLeft: 24 }}
        >
          <Stack direction="horizontal" align="center" style={{ gap: 4 }}>
            <StatusDot status={dotStatus} size="sm" glow={effectiveStatus === 'running'} />
            <Text
              as="span"
              style={{
                fontSize: 12,
                lineHeight: 1,
                fontFamily: 'var(--font-mono)',
                color:
                  effectiveStatus === 'done'
                    ? 'var(--color-status-healthy)'
                    : effectiveStatus === 'error'
                      ? 'var(--color-status-error)'
                      : 'var(--color-subtle)',
              }}
            >
              {effectiveStatus}
            </Text>
          </Stack>

          {canExpand ? (
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => setExpanded((v) => !v)}
              aria-expanded={expanded}
              aria-label={expanded ? 'Collapse' : 'Expand'}
              style={{ width: 22, height: 22 }}
            >
              <ChevronRightIcon open={expanded} />
            </Button>
          ) : (
            <Stack style={{ width: 22 }} />
          )}

          {tool.canvasViewId && (
            <Button
              type="button"
              onClick={handlePopout}
              style={{
                width: 22,
                height: 22,
                flexShrink: 0,
                background: isCanvasActive
                  ? 'color-mix(in oklch, var(--color-accent) 12%, transparent)'
                  : 'transparent',
                border: `1px solid ${isCanvasActive ? 'color-mix(in oklch, var(--color-accent) 35%, transparent)' : 'transparent'}`,
                borderRadius: 3,
                color: isCanvasActive ? 'var(--color-accent)' : 'var(--color-border)',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
              title="Open in canvas"
              aria-label={`Open ${tool.name} result in canvas`}
            >
              <PopoutIcon />
            </Button>
          )}
        </Stack>
      </Stack>

      {/* Expanded result */}
      {expanded && tool.result && (
        <Stack
          style={{
            borderTop: '1px solid var(--color-border-subtle)',
            padding: '8px 12px 8px 46px',
            fontSize: 12,
            fontFamily: 'var(--font-mono)',
            color: 'var(--color-muted)',
            lineHeight: 1.6,
          }}
        >
          {tool.result}
        </Stack>
      )}
    </Stack>
  );
});
