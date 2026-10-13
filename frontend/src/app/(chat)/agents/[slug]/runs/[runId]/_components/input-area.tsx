'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Floating input: context pills + textarea + accent send button + hint row.
 */

import { useState, useCallback } from 'react';
import { Stack, Text, Button, InputSurface, Textarea } from '@orbit/hades';
import { useRunContext } from '../_context/run-context';

// Context tools — toggleable pills that annotate the message with context sources
const CONTEXT_TOOLS = [
  {
    id: 'sandbox' as const,
    label: 'Sandbox',
    icon: (
      <svg
        width="12"
        height="12"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        aria-hidden
      >
        <polyline points="4 17 10 11 4 5" />
        <line x1="12" y1="19" x2="20" y2="19" />
      </svg>
    ),
  },
  {
    id: 'files' as const,
    label: 'Files',
    icon: (
      <svg
        width="12"
        height="12"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        aria-hidden
      >
        <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
        <polyline points="14 2 14 8 20 8" />
      </svg>
    ),
  },
  {
    id: 'memory' as const,
    label: 'Memory',
    icon: (
      <svg
        width="12"
        height="12"
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
    ),
  },
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

function StopIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" aria-hidden>
      <rect x="4" y="4" width="16" height="16" rx="2" />
    </svg>
  );
}

export function InputArea() {
  const { agentName, runState, isStarting, addHumanMessage, interruptRun } = useRunContext();
  const [value, setValue] = useState('');
  const [active, setActive] = useState<Set<ContextToolId>>(new Set());
  const isEnded =
    runState === 'completed' || runState === 'failed' || runState === 'budget_exhausted';
  // A starting run takes no messages. The API would answer "run is starting".
  // Typing is never blocked while the agent works: a message sent mid-turn is
  // read at the agent's next tool boundary.
  const isDisabled = isEnded || isStarting;
  const canSend = !!value.trim() && !isDisabled;
  // Only show Stop when the run is actually interruptible. The backend rejects an
  // interrupt unless the run is 'active' (and interruptRun swallows that error), so
  // showing Stop during 'starting'/pending made the button a silent no-op.
  const showStop = runState === 'active';

  function toggle(id: ContextToolId) {
    if (isDisabled) return;
    setActive((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  const handleSend = useCallback(() => {
    const content = value.trim();
    if (!content || isDisabled) return;
    addHumanMessage(content);
    setValue('');
  }, [value, addHumanMessage, isDisabled]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  return (
    <Stack style={{ flexShrink: 0, padding: '0 16px 10px', gap: 0 }}>
      <InputSurface
        data-floating=""
        style={{
          background: 'var(--color-surface-elevated)',
          boxShadow: 'var(--shadow-elevated), inset 0 0 0 1px var(--color-border-subtle)',
        }}
      >
        <Stack style={{ padding: '12px 14px 10px', gap: 0 }}>
          <Textarea
            variant="bare"
            autoResize
            maxRows={6}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={
              runState === 'budget_exhausted'
                ? `${agentName} run stopped: budget exhausted`
                : isEnded
                  ? `${agentName} run has ended`
                  : isStarting
                    ? `${agentName} is ${runState === 'pending' ? 'queued' : 'starting'}…`
                    : showStop
                      ? `Message ${agentName}. It reads this at its next step.`
                      : `Reply to ${agentName}…`
            }
            disabled={isDisabled}
            rows={2}
            style={{ fontSize: 14, lineHeight: 1.6, width: '100%', marginBottom: 12 }}
          />

          {/* Toolbar: context pills left · send right */}
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
                    disabled={isDisabled}
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

            <Stack direction="horizontal" align="center" style={{ gap: 6 }}>
              {showStop && (
                /* Stop button — interrupts the active turn */
                <Button
                  type="button"
                  onClick={interruptRun}
                  aria-label="Stop agent"
                  style={{
                    width: 36,
                    height: 36,
                    borderRadius: 9,
                    flexShrink: 0,
                    background: 'var(--color-surface-overlay)',
                    border: '1px solid var(--color-border)',
                    color: 'var(--color-foreground)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    transition: 'background 0.15s, color 0.15s',
                    cursor: 'pointer',
                  }}
                >
                  <StopIcon />
                </Button>
              )}
              {
                /* Accent rounded-square send button */
                <Button
                  type="button"
                  disabled={!canSend}
                  onClick={handleSend}
                  aria-label="Send message"
                  style={{
                    width: 36,
                    height: 36,
                    borderRadius: 9,
                    flexShrink: 0,
                    background: canSend ? 'var(--color-accent)' : 'var(--color-surface-overlay)',
                    border: `1px solid ${canSend ? 'transparent' : 'var(--color-border-subtle)'}`,
                    color: canSend ? 'var(--color-accent-foreground)' : 'var(--color-subtle)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    transition: 'background 0.15s, color 0.15s',
                  }}
                >
                  <SendIcon />
                </Button>
              }
            </Stack>
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
            style={{ fontSize: 12, color: 'var(--color-subtle)', fontFamily: 'var(--font-mono)' }}
          >
            send
          </Text>
        </Stack>
        <Stack direction="horizontal" align="center" style={{ gap: 4 }}>
          <KbdChip>⇧↵</KbdChip>
          <Text
            as="span"
            style={{ fontSize: 12, color: 'var(--color-subtle)', fontFamily: 'var(--font-mono)' }}
          >
            newline
          </Text>
        </Stack>
      </Stack>
    </Stack>
  );
}
