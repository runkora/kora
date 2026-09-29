/**
 * @tier 2 — Orbit-domain-aware. Agent run chat design exploration.
 * Iterate here without deploying. Run: pnpm storybook (port 6006)
 */

import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import {
  Stack,
  Text,
  Code,
  Button,
  Badge,
  StatusDot,
  ChatBubble,
  InputSurface,
  Textarea,
  StreamingCursor,
} from '@orbit/terra';

// ── Types ─────────────────────────────────────────────────────────────────────

type ToolIconType = 'terminal' | 'document' | 'database';

interface MockTool {
  id: string;
  name: string;
  args?: Record<string, string>;
  iconType?: ToolIconType;
  status: 'running' | 'done' | 'error';
  result?: string;
  timing?: string;
}

// ── SVG Icons ─────────────────────────────────────────────────────────────────

function OrbitalMark({ size = 9 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 12 12" fill="none" aria-hidden>
      <circle cx="6" cy="6" r="2.5" fill="currentColor" />
      <circle
        cx="6"
        cy="6"
        r="5"
        stroke="currentColor"
        strokeWidth="1.3"
        fill="none"
        opacity=".45"
      />
    </svg>
  );
}

function TerminalIcon({ size = 11 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden
    >
      <polyline points="4 17 10 11 4 5" />
      <line x1="12" y1="19" x2="20" y2="19" />
    </svg>
  );
}

function DocumentIcon({ size = 11 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden
    >
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <polyline points="14 2 14 8 20 8" />
    </svg>
  );
}

function DatabaseIcon({ size = 11 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden
    >
      <ellipse cx="12" cy="5" rx="9" ry="3" />
      <path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3" />
      <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5" />
    </svg>
  );
}

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

// Upward arrow — used in the accent send button
function SendIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <line x1="12" y1="19" x2="12" y2="5" />
      <polyline points="5 12 12 5 19 12" />
    </svg>
  );
}

// ── Tool icon config ───────────────────────────────────────────────────────────

const TOOL_ICON: Record<ToolIconType, { bg: string; color: string; el: React.ReactNode }> = {
  terminal: {
    bg: 'color-mix(in oklch, var(--color-status-healthy) 10%, transparent)',
    color: 'var(--color-status-healthy)',
    el: <TerminalIcon />,
  },
  document: {
    bg: 'color-mix(in oklch, var(--color-accent) 10%, transparent)',
    color: 'var(--color-accent)',
    el: <DocumentIcon />,
  },
  database: {
    bg: 'color-mix(in oklch, var(--color-status-degraded) 10%, transparent)',
    color: 'var(--color-status-degraded)',
    el: <DatabaseIcon />,
  },
};

// ── Topbar ────────────────────────────────────────────────────────────────────

function RunTopbar({
  agentName,
  model,
  runId,
  isActive,
}: {
  agentName: string;
  model: string;
  runId: string;
  isActive?: boolean;
}) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      style={{
        gap: 10,
        padding: '0 20px',
        height: 44,
        background: 'var(--color-surface)',
        borderBottom: '1px solid var(--color-border-subtle)',
        flexShrink: 0,
      }}
    >
      <Button type="button" variant="muted" size="icon-sm" aria-label="Back">
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          aria-hidden
        >
          <polyline points="15 18 9 12 15 6" />
        </svg>
      </Button>

      <Stack direction="horizontal" align="center" style={{ gap: 8 }}>
        <Stack
          align="center"
          justify="center"
          style={{
            width: 20,
            height: 20,
            flexShrink: 0,
            background: 'var(--color-surface-elevated)',
            borderRadius: 4,
            color: 'var(--color-accent)',
          }}
          aria-hidden
        >
          <OrbitalMark size={9} />
        </Stack>
        <Text
          as="span"
          style={{
            fontWeight: 600,
            fontSize: 13,
            color: 'var(--color-foreground)',
            letterSpacing: '-0.01em',
          }}
        >
          {agentName}
        </Text>
      </Stack>

      <Stack direction="horizontal" align="center" style={{ gap: 6 }}>
        <StatusDot
          status={isActive ? 'healthy' : 'idle'}
          size="sm"
          glow={isActive}
          aria-label={isActive ? 'Active' : 'Ended'}
        />
        <Badge variant="dim" size="sm" mono>
          {model}
        </Badge>
        <Badge variant="dim" size="sm" mono>
          {runId}
        </Badge>
      </Stack>

      <Stack grow />

      <Stack direction="horizontal" align="center" style={{ gap: 7 }}>
        <Stack
          style={{
            width: 52,
            height: 2,
            background: 'var(--color-border-subtle)',
            borderRadius: 2,
            overflow: 'hidden',
          }}
        >
          <Stack
            style={{
              height: '100%',
              width: '21%',
              background: 'var(--color-accent)',
              borderRadius: 2,
            }}
          />
        </Stack>
        <Text
          as="span"
          style={{ fontSize: 12, color: 'var(--color-subtle)', fontFamily: 'var(--font-mono)' }}
        >
          42k
        </Text>
      </Stack>

      <Button type="button" variant="outline" size="sm">
        <svg
          width="11"
          height="11"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          aria-hidden
        >
          <rect x="3" y="3" width="18" height="18" rx="2" />
          <line x1="12" y1="3" x2="12" y2="21" />
        </svg>
        Canvas
      </Button>
    </Stack>
  );
}

// ── Reasoning row ─────────────────────────────────────────────────────────────
// Flashing accent dot + thin vertical timeline line + gradient fade on expand.
// Not a blockquote — the line is a separate absolute element, not a border.

const REASONING_CSS = `
[data-reasoning-pulse] {
  animation: reasoning-pulse 1.6s ease-in-out infinite;
}
@keyframes reasoning-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.15; }
}
[data-floating] {
  border-color: transparent;
  transition: border-color 0.15s, box-shadow 0.15s;
}
[data-floating]:focus-within {
  border-color: var(--color-accent);
}
button[data-context-pill] {
  transition: background 0.12s, border-color 0.12s, color 0.12s;
}
button[data-context-pill]:not([data-active]):not(:disabled):hover {
  background: color-mix(in oklch, var(--color-accent) 8%, transparent);
  border-color: color-mix(in oklch, var(--color-accent) 25%, transparent);
  color: var(--color-accent);
}
button[data-context-pill][data-active] {
  background: color-mix(in oklch, var(--color-accent) 12%, transparent);
  border-color: color-mix(in oklch, var(--color-accent) 35%, transparent);
  color: var(--color-accent);
}
button[data-context-pill][data-active]:hover {
  background: color-mix(in oklch, var(--color-accent) 20%, transparent);
  border-color: color-mix(in oklch, var(--color-accent) 50%, transparent);
}
`;

function ReasoningRow({
  isActive,
  duration,
  content,
}: {
  isActive?: boolean;
  duration?: string;
  content?: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const canExpand = !isActive && !!content;
  const showLine = expanded || isActive;

  if (!isActive && !content) return null;

  return (
    <Stack style={{ marginBottom: 8, position: 'relative', gap: 0 }}>
      {/* Thin vertical timeline line — runs from below the dot through content */}
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
        {/* Dot — pulses while active, solid when done */}
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
          REASONING{duration ? ` · ${duration.toUpperCase()}` : isActive ? ' · …' : ''}
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

      {/* Expanded content — height-capped with gradient fade, not a blockquote */}
      {expanded && content && (
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
            {content}
          </Text>
          {/* Gradient fade — blends content into background */}
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

// ── Tool card ─────────────────────────────────────────────────────────────────
// Icon | name + args | timing · status | expand
// Expanded output uses borderTop, not a Separator, to avoid double-spacing.

function ToolCard({ tool }: { tool: MockTool }) {
  const [expanded, setExpanded] = useState(false);
  const iconCfg = TOOL_ICON[tool.iconType ?? 'document'];
  const canExpand = tool.status !== 'running' && !!tool.result;
  const dotStatus =
    tool.status === 'running' ? 'pending' : tool.status === 'done' ? 'healthy' : 'error';

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
        <Stack
          align="center"
          justify="center"
          style={{
            width: 24,
            height: 24,
            borderRadius: 5,
            flexShrink: 0,
            background: iconCfg.bg,
            color: iconCfg.color,
          }}
        >
          {iconCfg.el}
        </Stack>

        {/* Name + args — single truncated line */}
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
          {tool.args &&
            Object.entries(tool.args).map(([k, v], i) => (
              <Text key={k} as="span" style={{ fontSize: 12, lineHeight: 1 }}>
                {i === 0 ? '  ' : ''}
                {i > 0 && (
                  <Text
                    as="span"
                    style={{ fontSize: 12, lineHeight: 1, color: 'var(--color-border)' }}
                  >
                    ,{' '}
                  </Text>
                )}
                <Text
                  as="span"
                  style={{ fontSize: 12, lineHeight: 1, color: 'var(--color-subtle)' }}
                >
                  {k}=
                </Text>
                <Text
                  as="span"
                  style={{ fontSize: 12, lineHeight: 1, color: 'var(--color-muted)' }}
                >
                  &quot;{v}&quot;
                </Text>
              </Text>
            ))}
        </Text>

        {/* Right: timing · status · expand */}
        <Stack
          direction="horizontal"
          align="center"
          style={{ gap: 8, flexShrink: 0, marginLeft: 24 }}
        >
          {tool.timing && (
            <Text
              as="span"
              style={{
                fontSize: 12,
                lineHeight: 1,
                color: 'var(--color-subtle)',
                fontFamily: 'var(--font-mono)',
              }}
            >
              {tool.timing}
            </Text>
          )}
          <Stack direction="horizontal" align="center" style={{ gap: 4 }}>
            <StatusDot status={dotStatus} size="sm" glow={tool.status === 'running'} />
            <Text
              as="span"
              style={{
                fontSize: 12,
                lineHeight: 1,
                fontFamily: 'var(--font-mono)',
                color:
                  tool.status === 'done'
                    ? 'var(--color-status-healthy)'
                    : tool.status === 'error'
                      ? 'var(--color-status-error)'
                      : 'var(--color-subtle)',
              }}
            >
              {tool.status}
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
        </Stack>
      </Stack>

      {/* Expanded output — borderTop instead of Separator to avoid double-spacing */}
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
}

// ── Agent turn ────────────────────────────────────────────────────────────────
// Byline: avatar · name · model · timestamp
// Content: reasoning row, tool cards, prose — all at container edge.
// Prose is width-constrained (68ch); tool cards remain full-width.

function AgentTurn({
  agentName,
  model,
  timestamp,
  reasoning,
  reasoningDuration,
  tools,
  children,
  isGenerating,
}: {
  agentName: string;
  model: string;
  timestamp: string;
  reasoning?: string;
  reasoningDuration?: string;
  tools?: MockTool[];
  children?: React.ReactNode;
  isGenerating?: boolean;
}) {
  const hasTools = !!tools?.length;
  const hasContent = !!children;
  const reasoningActive = isGenerating && !hasTools && !hasContent;

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
          <OrbitalMark size={13} />
        </Stack>
        {/* Weight 500 in thread: prominent enough to identify, not competing with content */}
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
          · {timestamp}
        </Text>
      </Stack>

      {/* Content */}
      <Stack>
        <ReasoningRow isActive={reasoningActive} duration={reasoningDuration} content={reasoning} />

        {hasTools && (
          <Stack style={{ width: 'fit-content', gap: 4, marginBottom: hasContent ? 16 : 0 }}>
            {tools.map((t) => (
              <ToolCard key={t.id} tool={t} />
            ))}
          </Stack>
        )}

        {/* Prose — max 68ch keeps line length in the 45–75 char readable range */}
        {hasContent && (
          <Stack style={{ maxWidth: '68ch', gap: 8 }}>
            {children}
            {isGenerating && <StreamingCursor />}
          </Stack>
        )}
      </Stack>
    </Stack>
  );
}

// ── Human turn ────────────────────────────────────────────────────────────────

function HumanTurn({ children }: { children: React.ReactNode }) {
  return (
    <Stack style={{ padding: '8px 20px' }}>
      <ChatBubble direction="end" variant="accent">
        <Text
          as="span"
          style={{
            fontSize: 14,
            lineHeight: 1.6,
            wordBreak: 'break-word',
            overflowWrap: 'break-word',
          }}
        >
          {children}
        </Text>
      </ChatBubble>
    </Stack>
  );
}

// ── Session divider ───────────────────────────────────────────────────────────

function SessionDivider({ time }: { time: string }) {
  return (
    <Stack direction="horizontal" align="center" style={{ gap: 10, padding: '16px 20px 12px' }}>
      <Text
        as="span"
        style={{ flex: 1, height: '1px', background: 'var(--color-border-subtle)' }}
      />
      <Text
        as="span"
        style={{
          fontSize: 12,
          color: 'var(--color-subtle)',
          fontFamily: 'var(--font-mono)',
          flexShrink: 0,
        }}
      >
        {time}
      </Text>
      <Text
        as="span"
        style={{ flex: 1, height: '1px', background: 'var(--color-border-subtle)' }}
      />
    </Stack>
  );
}

// ── Input area ────────────────────────────────────────────────────────────────

// Toggleable context pills — user attaches context to their message
const CONTEXT_TOOLS = [
  { id: 'sandbox', label: 'Sandbox', icon: <TerminalIcon size={12} /> },
  { id: 'files', label: 'Files', icon: <DocumentIcon size={12} /> },
  { id: 'memory', label: 'Memory', icon: <DatabaseIcon size={12} /> },
] as const;

type ContextToolId = (typeof CONTEXT_TOOLS)[number]['id'];

// Small bordered keyboard hint chip
function KbdChip({ children }: { children: React.ReactNode }) {
  return (
    <Text
      as="span"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        border: '1px solid var(--color-border)',
        borderRadius: 3,
        padding: '0 4px',
        minWidth: 18,
        lineHeight: '16px',
        fontSize: 10,
        fontFamily: 'var(--font-mono)',
        color: 'var(--color-subtle)',
      }}
    >
      {children}
    </Text>
  );
}

function InputArea({
  agentName,
  disabled,
  variant = 'current',
}: {
  agentName: string;
  disabled?: boolean;
  variant?: 'current' | 'd' | 'd1' | 'd2' | 'd3' | 'd4' | 'd5';
}) {
  const [value, setValue] = useState('');
  const [active, setActive] = useState<Set<ContextToolId>>(new Set());
  const canSend = !!value.trim() && !disabled;

  function toggle(id: ContextToolId) {
    if (disabled) return;
    setActive((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function handleKey(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey && canSend) {
      e.preventDefault();
      setValue('');
    }
  }

  const SURFACE_VARIANTS: Record<string, React.CSSProperties> = {
    current: { boxShadow: '0 8px 32px rgb(0 0 0 / 0.28), 0 2px 8px rgb(0 0 0 / 0.16)' },
    // D — baseline winner: overlay surface, moderate shadow, faint inner glow
    d: {
      background: 'var(--color-surface-overlay)',
      boxShadow:
        '0 12px 36px rgb(0 0 0 / 0.4), 0 2px 10px rgb(0 0 0 / 0.2), inset 0 0 0 1px rgb(255 255 255 / 0.09)',
    },
    // D1 — more lift: taller offset, wider spread
    d1: {
      background: 'var(--color-surface-overlay)',
      boxShadow:
        '0 20px 52px rgb(0 0 0 / 0.5), 0 4px 16px rgb(0 0 0 / 0.24), inset 0 0 0 1px rgb(255 255 255 / 0.09)',
    },
    // D2 — warm shadow: accent hue bleeds into the outer shadow
    d2: {
      background: 'var(--color-surface-overlay)',
      boxShadow: `0 12px 40px color-mix(in oklch, var(--color-accent) 12%, rgb(0 0 0 / 0.5)), 0 2px 10px rgb(0 0 0 / 0.18), inset 0 0 0 1px rgb(255 255 255 / 0.09)`,
    },
    // D3 — crisper edge: tighter shadow + stronger inner glow
    d3: {
      background: 'var(--color-surface-elevated)',
      boxShadow:
        '0 8px 28px rgb(0 0 0 / 0.45), 0 2px 8px rgb(0 0 0 / 0.22), inset 0 0 0 1px rgb(255 255 255 / 0.16)',
    },
    // D4 — accent glow ring: inner edge tinted with brand colour
    d4: {
      background: 'var(--color-surface-overlay)',
      boxShadow: `0 12px 36px rgb(0 0 0 / 0.4), 0 2px 10px rgb(0 0 0 / 0.2), inset 0 0 0 1px color-mix(in oklch, var(--color-accent) 28%, rgb(255 255 255 / 0.05))`,
    },
    // D5 — diffuse: wide spread, low opacity — softest possible
    d5: {
      background: 'var(--color-surface-overlay)',
      boxShadow:
        '0 6px 56px rgb(0 0 0 / 0.32), 0 1px 6px rgb(0 0 0 / 0.16), inset 0 0 0 1px rgb(255 255 255 / 0.09)',
    },
  };
  const surfaceStyle = SURFACE_VARIANTS[variant] ?? SURFACE_VARIANTS.current;

  return (
    <Stack style={{ flexShrink: 0, padding: '0 16px 10px', gap: 0 }}>
      <InputSurface data-floating="" style={surfaceStyle}>
        <Stack style={{ padding: '12px 14px 10px', gap: 0 }}>
          {/* Text input */}
          <Textarea
            variant="bare"
            rows={2}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={handleKey}
            placeholder={
              disabled ? `${agentName} run has ended` : `Reply, or press / for commands…`
            }
            disabled={disabled}
            style={{
              fontSize: 14,
              lineHeight: 1.6,
              width: '100%',
              marginBottom: 12,
              resize: 'none',
            }}
          />

          {/* Toolbar: pills left · send right */}
          <Stack direction="horizontal" align="center" justify="between" style={{ gap: 8 }}>
            <Stack direction="horizontal" align="center" style={{ gap: 5 }}>
              {CONTEXT_TOOLS.map((tool) => {
                const isActive = active.has(tool.id);
                return (
                  <Button
                    key={tool.id}
                    type="button"
                    size="sm"
                    shape="pill"
                    variant="outline"
                    disabled={disabled}
                    onClick={() => toggle(tool.id)}
                    data-context-pill=""
                    data-active={isActive ? '' : undefined}
                  >
                    {tool.icon}
                    {tool.label}
                  </Button>
                );
              })}
            </Stack>

            {/* Accent rounded-square send button */}
            <Button
              type="button"
              disabled={!canSend}
              onClick={() => setValue('')}
              style={{
                width: 36,
                height: 36,
                borderRadius: 9,
                flexShrink: 0,
                background: canSend ? 'var(--color-accent)' : 'var(--color-surface-elevated)',
                border: `1px solid ${canSend ? 'transparent' : 'var(--color-border-subtle)'}`,
                color: canSend ? 'white' : 'var(--color-subtle)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                transition: 'background 0.15s, color 0.15s',
              }}
            >
              <SendIcon />
            </Button>
          </Stack>
        </Stack>
      </InputSurface>

      {/* Hint row */}
      <Stack
        direction="horizontal"
        align="center"
        style={{ marginTop: 8, gap: 12, paddingLeft: 2 }}
      >
        <Stack direction="horizontal" align="center" style={{ gap: 4 }}>
          <KbdChip>↵</KbdChip>
          <Text
            as="span"
            style={{ fontSize: 11, color: 'var(--color-subtle)', fontFamily: 'var(--font-mono)' }}
          >
            send
          </Text>
        </Stack>
        <Stack direction="horizontal" align="center" style={{ gap: 4 }}>
          <KbdChip>⇧↵</KbdChip>
          <Text
            as="span"
            style={{ fontSize: 11, color: 'var(--color-subtle)', fontFamily: 'var(--font-mono)' }}
          >
            newline
          </Text>
        </Stack>
        <Stack direction="horizontal" align="center" style={{ gap: 4 }}>
          <KbdChip>/</KbdChip>
          <Text
            as="span"
            style={{ fontSize: 11, color: 'var(--color-subtle)', fontFamily: 'var(--font-mono)' }}
          >
            commands
          </Text>
        </Stack>
      </Stack>
    </Stack>
  );
}

// ── Mock data ─────────────────────────────────────────────────────────────────

const AGENT = 'Security Reviewer';
const MODEL = 'claude-haiku-4-5';

const REASONING =
  "I should read package.json first to understand the project structure, then grep for common secret patterns. Environment files are typically gitignored so I'll focus on tracked source files — API_KEY, SECRET, and PASSWORD are the highest-signal patterns to check.";

const TOOLS = {
  provision: {
    id: 't1',
    name: 'provision_sandbox',
    iconType: 'database' as const,
    status: 'done' as const,
    timing: '1.2s',
    result: 'ready at /workspaces/cdca7326',
  },
  read: {
    id: 't2',
    name: 'read',
    iconType: 'document' as const,
    args: { file: 'package.json' },
    status: 'done' as const,
    timing: '0.1s',
    result: '42 lines — next.js 15, typescript, @orbit/terra',
  },
  grep: {
    id: 't3',
    name: 'bash',
    iconType: 'terminal' as const,
    args: { cmd: 'grep -rn "API_KEY|SECRET|PASSWORD" src/' },
    status: 'done' as const,
    timing: '2.1s',
    result: '0 matches',
  },
  grepRunning: {
    id: 't3',
    name: 'bash',
    iconType: 'terminal' as const,
    args: { cmd: 'grep -rn "API_KEY|SECRET|PASSWORD" src/' },
    status: 'running' as const,
  },
};

// ── Layout ────────────────────────────────────────────────────────────────────

function RunLayout({ isLive }: { isLive?: boolean }) {
  return (
    <Stack
      style={{
        height: '100vh',
        overflow: 'hidden',
        background: 'var(--color-background)',
        color: 'var(--color-foreground)',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <style>{REASONING_CSS}</style>
      <RunTopbar agentName={AGENT} model={MODEL} runId="9bb80e2e" isActive={isLive} />

      <Stack style={{ flex: 1, overflowY: 'auto', paddingBottom: 16 }}>
        <SessionDivider time="22:38" />

        <HumanTurn>Hello</HumanTurn>
        <HumanTurn>https://clearroute.io</HumanTurn>
        <HumanTurn>
          {
            "Can you do a thorough security audit of this entire repository? I want you to check for hardcoded secrets, exposed API keys, insecure dependencies, any SQL injection vectors, XSS vulnerabilities in the frontend, and make sure all auth middleware is correctly applied to every route. Also check that we're not accidentally logging sensitive data anywhere in the request/response cycle."
          }
        </HumanTurn>

        <HumanTurn>Hi, can you open a sandbox and tell me what tools you have available</HumanTurn>

        <AgentTurn agentName={AGENT} model={MODEL} timestamp="22:38:04" tools={[TOOLS.provision]}>
          {/*
            Prose intent: natural sentence, not a definition list.
            Inline mono for identifiers — no Code boxes, which read as interactive.
            Line length ~55 chars at 68ch max-width — within the 45–75 rule.
          */}
          <Text style={{ fontSize: 14, lineHeight: 1.65, color: 'var(--color-foreground)' }}>
            Sandbox is ready. You have access to <Code style={{ fontSize: 12 }}>bash</Code>,{' '}
            <Code style={{ fontSize: 12 }}>edit</Code>, <Code style={{ fontSize: 12 }}>read</Code>,
            and <Code style={{ fontSize: 12 }}>web_search</Code> in{' '}
            <Code style={{ fontSize: 12 }}>/workspaces/cdca7326</Code>.
          </Text>
        </AgentTurn>

        <HumanTurn>Can you check if there are any exposed secrets in this repo?</HumanTurn>

        {isLive ? (
          <AgentTurn
            agentName={AGENT}
            model={MODEL}
            timestamp="22:38:41"
            reasoning={REASONING}
            reasoningDuration="3s"
            tools={[TOOLS.read, TOOLS.grepRunning]}
            isGenerating
          />
        ) : (
          <AgentTurn
            agentName={AGENT}
            model={MODEL}
            timestamp="22:38:41"
            reasoning={REASONING}
            reasoningDuration="3s"
            tools={[TOOLS.read, TOOLS.grep]}
          >
            <Text style={{ fontSize: 14, lineHeight: 1.65, color: 'var(--color-foreground)' }}>
              No secrets found. All credentials are loaded from environment variables — nothing is
              hardcoded in <Code style={{ fontSize: 12 }}>src/</Code>.
            </Text>
            {/* Supporting detail: muted, same size, clearly secondary */}
            <Text style={{ fontSize: 14, lineHeight: 1.65, color: 'var(--color-muted)' }}>
              Scanned for <Code style={{ fontSize: 12 }}>API_KEY</Code>,{' '}
              <Code style={{ fontSize: 12 }}>SECRET</Code>,{' '}
              <Code style={{ fontSize: 12 }}>PASSWORD</Code> — 0 matches.
            </Text>
          </AgentTurn>
        )}
      </Stack>

      <InputArea agentName={AGENT} disabled={!isLive} variant="d3" />
    </Stack>
  );
}

// ── Meta ──────────────────────────────────────────────────────────────────────

const meta: Meta = {
  title: 'App/Runs/AgentRunChat',
  parameters: {
    layout: 'fullscreen',
  },
};

export default meta;
type Story = StoryObj;

export const Complete: Story = {
  name: 'Complete run',
  render: () => <RunLayout />,
};

export const Live: Story = {
  name: 'Live — streaming',
  render: () => <RunLayout isLive />,
};

const VARIANTS: { key: 'd' | 'd1' | 'd2' | 'd3' | 'd4' | 'd5'; label: string }[] = [
  { key: 'd', label: 'D  — baseline · overlay surface · moderate shadow · faint inner glow' },
  { key: 'd1', label: 'D1 — more lift · taller offset · wider spread' },
  { key: 'd2', label: 'D2 — warm shadow · accent hue bleeds into shadow' },
  { key: 'd3', label: 'D3 — crisper edge · tighter shadow · stronger inner glow' },
  { key: 'd4', label: 'D4 — accent glow ring · inner edge tinted brand colour' },
  { key: 'd5', label: 'D5 — diffuse · wide spread · softest possible' },
];

export const InputComparison: Story = {
  name: 'Input — variants',
  render: () => (
    <Stack
      style={{
        minHeight: '100vh',
        background: 'var(--color-background)',
        padding: '40px 0 0',
        gap: 0,
      }}
    >
      <style>{REASONING_CSS}</style>
      {VARIANTS.map(({ key, label }, i) => (
        <Stack key={key} style={{ gap: 0 }}>
          <Stack style={{ padding: `${i === 0 ? 0 : 28}px 32px 10px`, gap: 0 }}>
            <Text
              as="span"
              style={{
                fontSize: 11,
                color: 'var(--color-subtle)',
                fontFamily: 'var(--font-mono)',
                letterSpacing: '0.07em',
              }}
            >
              {label.toUpperCase()}
            </Text>
          </Stack>
          <InputArea agentName={AGENT} variant={key} />
        </Stack>
      ))}
    </Stack>
  ),
};
