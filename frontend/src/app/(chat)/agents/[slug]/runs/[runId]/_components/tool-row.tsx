'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Core tool call row: icon + name + arg + status badge + pop-out button.
 * Left border colour reflects tool status; pop-out button opens the canvas view.
 */

import { useState } from 'react';
import { Stack, Text, Button } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';
import type { ToolCall } from '../_types';

// Border colours by status
const BORDER_COLORS = {
  running: 'oklch(50% 0.1 80 / 0.6)',
  done: 'oklch(45% 0.1 140 / 0.5)',
  error: 'oklch(45% 0.15 25 / 0.5)',
} as const;

// Inline orbital spinner keyframes — injected once per page
// Uses data-spin attribute selector to avoid className on Terra components
export const TOOL_SPIN_CSS = `
[data-spin] {
  display: inline-flex; align-items: center; justify-content: center;
  width: 14px; height: 14px; position: relative; flex-shrink: 0;
  vertical-align: middle;
}
[data-spin]::before {
  content: '';
  position: absolute; width: 10px; height: 10px;
  border-radius: 50%;
  border: 1px solid oklch(72% 0.17 8 / 0.25);
}
[data-spin]::after {
  content: '';
  position: absolute; width: 4px; height: 4px;
  border-radius: 50%;
  background: var(--chat-degraded);
  top: 50%; left: 50%; margin: -2px 0 0 -2px;
  animation: tool-orb 1.4s linear infinite;
  transform-origin: 0 0;
}
@keyframes tool-orb {
  from { transform: rotate(0deg) translateX(5px); }
  to   { transform: rotate(360deg) translateX(5px); }
}
`;

// Pop-out icon SVG
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

export function ToolRow({ tool }: { tool: ToolCall }) {
  const { canvasState, openCanvas } = useRunContext();
  const [hovered, setHovered] = useState(false);

  const isCanvasActive =
    tool.canvasViewId !== undefined && canvasState.activeTabId === tool.canvasViewId;

  const borderColor = isCanvasActive
    ? 'var(--chat-acc)'
    : (BORDER_COLORS[tool.status] ?? 'var(--chat-bds)');

  function handlePopout(e: React.MouseEvent) {
    e.stopPropagation();
    if (tool.canvasViewId) openCanvas(tool.canvasViewId);
  }

  return (
    <Stack
      direction="horizontal"
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        margin: '3px 0',
        borderLeft: `2px solid ${borderColor}`,
        transition: 'border-color 0.15s',
        position: 'relative',
      }}
    >
      {/* Inner content */}
      <Stack grow style={{ paddingLeft: 10, minWidth: 0 }}>
        <Stack
          direction="horizontal"
          align="center"
          style={{
            gap: 7,
            padding: '3px 0',
            userSelect: 'none',
          }}
        >
          {/* Emoji icon */}
          <Text as="span" style={{ fontSize: 12, opacity: 0.8, flexShrink: 0 }}>
            {tool.icon}
          </Text>

          {/* Tool name */}
          <Text
            as="span"
            style={{
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              fontSize: 12,
              color: 'var(--chat-muted)',
              flexShrink: 0,
            }}
          >
            {tool.name}
          </Text>

          {/* Argument (truncated) */}
          <Text
            as="span"
            style={{
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              fontSize: 11.5,
              color: 'var(--chat-subtle)',
              flex: 1,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              minWidth: 0,
            }}
          >
            {tool.arg}
          </Text>

          {/* Status badge */}
          <Text
            as="span"
            style={{
              fontSize: 11,
              display: 'flex',
              alignItems: 'center',
              gap: 3,
              flexShrink: 0,
              color:
                tool.status === 'running'
                  ? 'var(--chat-degraded)'
                  : tool.status === 'done'
                    ? 'var(--chat-healthy)'
                    : 'var(--chat-error)',
            }}
          >
            {tool.status === 'running' ? (
              <>
                <Text as="span" data-spin="" aria-label="Running" />
                running
              </>
            ) : (
              tool.result
            )}
          </Text>

          {/* Pop-out button (⤢) */}
          {tool.canvasViewId && (
            <Button
              type="button"
              onClick={handlePopout}
              style={{
                width: 22,
                height: 22,
                flexShrink: 0,
                background: isCanvasActive ? 'var(--chat-acc-dim)' : 'transparent',
                border: `1px solid ${isCanvasActive ? 'var(--chat-acc-bdr)' : 'transparent'}`,
                borderRadius: 3,
                color: isCanvasActive ? 'var(--chat-acc)' : 'var(--chat-bds)',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                opacity: isCanvasActive || hovered ? 1 : 0,
                transition: 'opacity 0.15s',
              }}
              title="Open in canvas"
              aria-label={`Open ${tool.name} result in canvas`}
            >
              <PopoutIcon />
            </Button>
          )}
        </Stack>
      </Stack>
    </Stack>
  );
}
