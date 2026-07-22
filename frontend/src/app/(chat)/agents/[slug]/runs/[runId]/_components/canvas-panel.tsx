'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Slide-in canvas panel (0 → 520px). Shows code, diff, and terminal views
 * in tabs, with an activity log at the bottom.
 */

import { Stack, Text, Button } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';
import type { CanvasView, CanvasTab, DiffLine, TerminalLine, Turn } from '../_types';

// ── Canvas tab ────────────────────────────────────────────────────────────────

function CanvasTabItem({
  tab,
  isActive,
  onClick,
}: {
  tab: CanvasTab;
  isActive: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      onClick={onClick}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 5,
        padding: '0 12px',
        height: 36,
        flexShrink: 0,
        fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
        fontSize: 11.5,
        color: isActive ? 'var(--chat-fg)' : 'var(--chat-subtle)',
        cursor: 'pointer',
        borderRight: '1px solid var(--chat-bds)',
        background: isActive ? 'var(--chat-bg)' : 'transparent',
        outline: 'none',
        position: 'relative',
        whiteSpace: 'nowrap',
      }}
    >
      {tab.ext && (
        <Text as="span" style={{ color: 'var(--chat-acc)', opacity: 0.7 }}>
          {tab.ext}
        </Text>
      )}
      {tab.label}
      {tab.modified && (
        <Text as="span" style={{ color: 'var(--chat-degraded)', fontSize: 9 }}>
          ●
        </Text>
      )}
      {/* Active accent bottom border */}
      {isActive && (
        <Text
          as="span"
          style={{
            position: 'absolute',
            bottom: 0,
            left: 0,
            right: 0,
            height: 2,
            background: 'var(--chat-acc)',
          }}
          aria-hidden
        />
      )}
    </Button>
  );
}

// ── Code view ─────────────────────────────────────────────────────────────────

function CodeView({ view }: { view: CanvasView }) {
  const lines = (view.code ?? '').split('\n');
  return (
    <Stack style={{ display: 'flex', flexDirection: 'column', flex: 1, overflow: 'auto' }}>
      {/* Header */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 8,
          padding: '7px 14px',
          flexShrink: 0,
          background: 'var(--chat-surf)',
          borderBottom: '1px solid var(--chat-bds)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 11.5,
        }}
      >
        {view.subtitle && (
          <Text as="span" style={{ color: 'var(--chat-subtle)' }}>
            {view.subtitle}
          </Text>
        )}
        <Text as="span" style={{ color: 'var(--chat-muted)' }}>
          {view.title}
        </Text>
        <Text as="span" style={{ flex: 1 }} />
        {view.lineCount && (
          <Text as="span" style={{ color: 'var(--chat-subtle)', fontSize: 11 }}>
            {view.lineCount} lines
          </Text>
        )}
      </Stack>

      {/* Code block with line numbers */}
      <Stack
        style={{
          flex: 1,
          overflow: 'auto',
          background: 'var(--chat-deep)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 12.5,
          lineHeight: 1.7,
        }}
      >
        {lines.map((line, i) => (
          <Stack key={i} direction="horizontal">
            <Text
              as="span"
              style={{
                width: 36,
                paddingRight: 8,
                textAlign: 'right',
                color: 'var(--chat-bds)',
                fontSize: 11,
                userSelect: 'none',
                flexShrink: 0,
              }}
            >
              {i + 1}
            </Text>
            <Text
              as="span"
              style={{
                padding: '0 8px 0 4px',
                whiteSpace: 'pre',
                color: 'var(--chat-muted)',
                flex: 1,
              }}
            >
              {line}
            </Text>
          </Stack>
        ))}
      </Stack>
    </Stack>
  );
}

// ── Diff view ─────────────────────────────────────────────────────────────────

function DiffLineRow({ line }: { line: DiffLine }) {
  const isAdd = line.kind === 'add';
  const isDel = line.kind === 'del';
  const sigil = isAdd ? '+' : isDel ? '-' : ' ';

  return (
    <Stack
      direction="horizontal"
      style={{
        background: isAdd
          ? 'color-mix(in oklch, var(--color-status-healthy) 13%, transparent)'
          : isDel
            ? 'color-mix(in oklch, var(--color-status-error) 13%, transparent)'
            : 'transparent',
      }}
    >
      {/* Old line number */}
      <Text
        as="span"
        style={{
          width: 36,
          padding: '0 6px',
          textAlign: 'right',
          fontSize: 11,
          userSelect: 'none',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-end',
          color: isDel
            ? 'color-mix(in oklch, var(--color-status-error) 50%, transparent)'
            : 'var(--chat-bds)',
        }}
      >
        {line.oldNum ?? ''}
      </Text>
      {/* New line number */}
      <Text
        as="span"
        style={{
          width: 36,
          padding: '0 6px',
          textAlign: 'right',
          fontSize: 11,
          userSelect: 'none',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-end',
          color: isAdd
            ? 'color-mix(in oklch, var(--color-status-healthy) 50%, transparent)'
            : 'var(--chat-bds)',
        }}
      >
        {line.newNum ?? ''}
      </Text>
      {/* Sigil */}
      <Text
        as="span"
        style={{
          width: 16,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: 13,
          flexShrink: 0,
          color: isAdd ? 'var(--chat-healthy)' : isDel ? 'var(--chat-error)' : 'transparent',
        }}
      >
        {sigil}
      </Text>
      {/* Content */}
      <Text
        as="span"
        style={{
          padding: '0 8px 0 4px',
          whiteSpace: 'pre',
          flex: 1,
          color: isAdd
            ? 'var(--color-status-healthy)'
            : isDel
              ? 'var(--color-status-error)'
              : 'var(--chat-subtle)',
        }}
      >
        {line.content}
      </Text>
    </Stack>
  );
}

function DiffView({ view }: { view: CanvasView }) {
  return (
    <Stack style={{ display: 'flex', flexDirection: 'column', flex: 1, overflow: 'auto' }}>
      {/* Header */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 8,
          padding: '7px 14px',
          flexShrink: 0,
          background: 'var(--chat-surf)',
          borderBottom: '1px solid var(--chat-bds)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 11.5,
        }}
      >
        {view.subtitle && (
          <Text as="span" style={{ color: 'var(--chat-subtle)' }}>
            {view.subtitle}
          </Text>
        )}
        <Text as="span" style={{ color: 'var(--chat-muted)' }}>
          {view.title}
        </Text>
        <Text as="span" style={{ flex: 1 }} />
        {/* Diff stat */}
        <Stack direction="horizontal" style={{ gap: 6, fontSize: 11 }}>
          {view.addCount !== undefined && (
            <Text as="span" style={{ color: 'var(--chat-healthy)' }}>
              +{view.addCount}
            </Text>
          )}
          {view.delCount !== undefined && (
            <Text as="span" style={{ color: 'var(--chat-error)' }}>
              -{view.delCount}
            </Text>
          )}
        </Stack>
        <Button
          type="button"
          style={{
            padding: '2px 8px',
            background: 'var(--chat-surf2)',
            border: '1px solid var(--chat-bds)',
            borderRadius: 3,
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            fontSize: 11,
            color: 'var(--chat-subtle)',
            cursor: 'pointer',
          }}
        >
          copy
        </Button>
      </Stack>

      {/* Diff lines */}
      <Stack
        style={{
          flex: 1,
          overflow: 'auto',
          background: 'var(--chat-deep)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 12.5,
          lineHeight: 1.7,
        }}
      >
        {(view.diffLines ?? []).map((line, i) => (
          <DiffLineRow key={i} line={line} />
        ))}
      </Stack>
    </Stack>
  );
}

// ── Terminal view ─────────────────────────────────────────────────────────────

function TerminalLineRow({ line }: { line: TerminalLine }) {
  const colorMap: Record<TerminalLine['kind'], string> = {
    normal: 'var(--chat-muted)',
    error: 'var(--chat-error)',
    success: 'var(--chat-healthy)',
    dim: 'var(--chat-subtle)',
  };
  return <Stack style={{ color: colorMap[line.kind] }}>{line.content}</Stack>;
}

function TerminalView({ view }: { view: CanvasView }) {
  return (
    <Stack style={{ display: 'flex', flexDirection: 'column', flex: 1, overflow: 'auto' }}>
      {/* Header */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 8,
          padding: '7px 14px',
          flexShrink: 0,
          background: 'var(--chat-surf)',
          borderBottom: '1px solid var(--chat-bds)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 11.5,
        }}
      >
        <Text as="span" style={{ color: 'var(--chat-subtle)' }}>
          terminal
        </Text>
        <Text as="span" style={{ flex: 1 }} />
        {view.command && (
          <Text as="span" style={{ color: 'var(--chat-muted)', fontSize: 11 }}>
            {view.command}
          </Text>
        )}
        {view.exitCode !== undefined && (
          <Text
            as="span"
            style={{
              fontSize: 11,
              color: view.exitCode === 0 ? 'var(--chat-healthy)' : 'var(--chat-error)',
            }}
          >
            exit {view.exitCode}
          </Text>
        )}
      </Stack>

      {/* Terminal content */}
      <Stack
        style={{
          flex: 1,
          overflow: 'auto',
          background: 'var(--color-background)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 12.5,
          lineHeight: 1.65,
          padding: '12px 14px',
          color: 'var(--chat-muted)',
        }}
      >
        {(view.output ?? []).map((line, i) => (
          <TerminalLineRow key={i} line={line} />
        ))}
      </Stack>
    </Stack>
  );
}

// ── Canvas view renderer (dispatches by type) ─────────────────────────────────

function CanvasViewRenderer({ view }: { view: CanvasView }) {
  if (view.type === 'code') return <CodeView view={view} />;
  if (view.type === 'diff') return <DiffView view={view} />;
  if (view.type === 'terminal') return <TerminalView view={view} />;
  return null;
}

// ── Activity log ──────────────────────────────────────────────────────────────

function ActivityLog({ turns, isLive }: { turns: Turn[]; isLive: boolean }) {
  // Collect all tool calls in document order
  const allTools = turns
    .filter((t) => t.type === 'agent')
    .flatMap((t) => {
      if (t.type !== 'agent') return [];
      return t.elements.flatMap((el) => (el.kind === 'tool' ? [el.tool] : []));
    });

  return (
    <Stack
      style={{
        borderTop: '1px solid var(--chat-bds)',
        background: 'var(--chat-deep)',
        flexShrink: 0,
        maxHeight: 110,
        overflowY: 'auto',
        paddingBottom: 6,
      }}
    >
      {/* Log header */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          padding: '6px 14px 5px',
          fontSize: 10,
          textTransform: 'uppercase',
          letterSpacing: '0.07em',
          color: 'var(--chat-subtle)',
          borderBottom: '1px solid var(--chat-bds)',
          marginBottom: 3,
          gap: 8,
        }}
      >
        Agent activity
        {isLive && (
          <Text
            as="span"
            style={{ color: 'var(--chat-degraded)', display: 'flex', alignItems: 'center', gap: 4 }}
          >
            <Text as="span" data-spin="" aria-hidden />
            live
          </Text>
        )}
      </Stack>

      {/* Log rows */}
      {allTools.map((tool) => (
        <Stack
          key={tool.id}
          direction="horizontal"
          align="center"
          style={{
            gap: 7,
            padding: '1px 14px',
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            fontSize: 11,
            color: 'var(--chat-subtle)',
          }}
        >
          <Text as="span" style={{ color: 'var(--chat-bds)', fontSize: 10, flexShrink: 0 }}>
            {/* Timestamp placeholder — in mock we don't track per-tool timestamps */}
            --:--
          </Text>
          <Text as="span" style={{ color: 'var(--chat-muted)', flexShrink: 0 }}>
            {tool.name}
          </Text>
          <Text
            as="span"
            style={{
              flex: 1,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {tool.arg}
          </Text>
          {/* Result icon */}
          {tool.status === 'running' && <Text as="span" data-spin="" aria-label="Running" />}
          {tool.status === 'done' && (
            <Text as="span" style={{ color: 'var(--chat-healthy)' }}>
              ✓
            </Text>
          )}
          {tool.status === 'error' && (
            <Text as="span" style={{ color: 'var(--chat-error)' }}>
              ✗
            </Text>
          )}
        </Stack>
      ))}
    </Stack>
  );
}

// ── CanvasPanel ───────────────────────────────────────────────────────────────

export function CanvasPanel() {
  const { canvasState, views, turns, runState, closeCanvas, switchTab } = useRunContext();
  const { isOpen, tabs, activeTabId } = canvasState;

  const activeView = activeTabId ? views[activeTabId] : null;
  const isLive = runState === 'active' || runState === 'starting';

  return (
    <Stack
      style={{
        width: isOpen ? 520 : 0,
        flexShrink: 0,
        display: 'flex',
        flexDirection: 'column',
        background: 'var(--chat-deep)',
        borderLeft: isOpen ? '1px solid var(--chat-bds)' : '1px solid transparent',
        overflow: 'hidden',
        transition: 'width 0.25s ease, border-color 0.25s ease',
      }}
      aria-hidden={!isOpen}
    >
      {/* Canvas topbar — 36px */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 0,
          height: 36,
          flexShrink: 0,
          background: 'var(--chat-surf)',
          borderBottom: '1px solid var(--chat-bds)',
        }}
      >
        {/* Scrollable tab bar */}
        <Stack
          direction="horizontal"
          grow
          style={{
            overflowX: 'auto',
            // Hide scrollbar
            scrollbarWidth: 'none',
          }}
        >
          {tabs.map((tab) => (
            <CanvasTabItem
              key={tab.id}
              tab={tab}
              isActive={activeTabId === tab.id}
              onClick={() => switchTab(tab.id)}
            />
          ))}
        </Stack>

        {/* Close button */}
        <Button
          type="button"
          onClick={closeCanvas}
          style={{
            width: 24,
            height: 24,
            flexShrink: 0,
            background: 'transparent',
            border: 'none',
            borderRadius: 4,
            color: 'var(--chat-subtle)',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: 16,
            marginRight: 8,
          }}
          aria-label="Close canvas"
        >
          ×
        </Button>
      </Stack>

      {/* Canvas content — flex 1 */}
      <Stack style={{ flex: 1, overflow: 'auto', display: 'flex', flexDirection: 'column' }}>
        {activeView ? (
          <CanvasViewRenderer view={activeView} />
        ) : (
          <Stack
            align="center"
            justify="center"
            style={{
              flex: 1,
              color: 'var(--chat-subtle)',
              fontSize: 12,
            }}
          >
            Click ⤢ on any tool row to open it here
          </Stack>
        )}
      </Stack>

      {/* Activity log — max 110px */}
      <ActivityLog turns={turns} isLive={isLive} />
    </Stack>
  );
}
