'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Fixed bottom input area: slash command chips + auto-resize textarea + send button.
 */

import { useRef, useState, useCallback } from 'react';
import { Stack, Text, Button, Code } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';

const SLASH_COMMANDS = ['/continue', '/approve', '/reject', '/checkpoint', '/explain'] as const;

// Send arrow icon
function SendIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      aria-hidden
    >
      <line x1="22" y1="2" x2="11" y2="13" />
      <polygon points="22 2 15 22 11 13 2 9 22 2" />
    </svg>
  );
}

export function InputArea() {
  const { addHumanMessage } = useRunContext();
  const [value, setValue] = useState('');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleInput = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 110)}px`;
  }, []);

  const handleSend = useCallback(() => {
    const content = value.trim();
    if (!content) return;
    addHumanMessage(content);
    setValue('');
    // Reset textarea height
    const el = textareaRef.current;
    if (el) el.style.height = 'auto';
  }, [value, addHumanMessage]);

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
    <Stack
      style={{
        borderTop: '1px solid var(--chat-bds)',
        background: 'var(--chat-surf)',
        flexShrink: 0,
        padding: '10px 16px 12px',
      }}
    >
      {/* Slash command chips */}
      <Stack
        direction="horizontal"
        style={{
          gap: 5,
          marginBottom: 8,
          overflowX: 'auto',
          scrollbarWidth: 'none',
        }}
      >
        {SLASH_COMMANDS.map((cmd) => (
          <Button
            key={cmd}
            type="button"
            onClick={() => setValue(cmd + ' ')}
            style={{
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              fontSize: 11,
              padding: '2px 8px',
              background: 'var(--chat-surf2)',
              border: '1px solid var(--chat-bds)',
              borderRadius: 20,
              color: 'var(--chat-subtle)',
              cursor: 'pointer',
              whiteSpace: 'nowrap',
            }}
          >
            {cmd}
          </Button>
        ))}
      </Stack>

      {/* Textarea wrapper */}
      <Stack
        direction="horizontal"
        align="end"
        style={{
          gap: 8,
          background: 'var(--chat-surf2)',
          border: '1px solid var(--chat-bds)',
          borderRadius: 8,
          padding: '9px 10px',
        }}
      >
        {/* Auto-resize textarea */}
        <textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onInput={handleInput}
          onKeyDown={handleKeyDown}
          placeholder="Message Orbit AI…"
          rows={1}
          style={{
            flex: 1,
            background: 'transparent',
            border: 'none',
            outline: 'none',
            resize: 'none',
            fontFamily: 'ui-sans-serif, system-ui, -apple-system, sans-serif',
            fontSize: 13.5,
            color: 'var(--chat-fg)',
            lineHeight: 1.5,
            minHeight: 20,
            maxHeight: 110,
            overflowY: 'auto',
          }}
        />

        {/* @ file button */}
        <Button
          type="button"
          style={{
            fontSize: 11,
            padding: '3px 7px',
            background: 'transparent',
            border: '1px solid var(--chat-bds)',
            borderRadius: 4,
            color: 'var(--chat-subtle)',
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            cursor: 'pointer',
            flexShrink: 0,
          }}
        >
          @ file
        </Button>

        {/* Send button */}
        <Button
          type="button"
          onClick={handleSend}
          disabled={!value.trim()}
          style={{
            width: 29,
            height: 29,
            background: value.trim() ? 'var(--chat-acc)' : 'var(--chat-surf3)',
            border: 'none',
            borderRadius: 4,
            cursor: value.trim() ? 'pointer' : 'not-allowed',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'oklch(9% 0.005 248)',
            flexShrink: 0,
            transition: 'background 0.15s',
          }}
          aria-label="Send message"
        >
          <SendIcon />
        </Button>
      </Stack>

      {/* Footer hints */}
      <Stack
        direction="horizontal"
        justify="between"
        style={{
          marginTop: 7,
          fontSize: 11,
          color: 'var(--chat-bds)',
        }}
      >
        <Text as="span">
          <Code
            style={{
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              fontSize: 10,
              padding: '1px 4px',
              background: 'var(--chat-surf3)',
              border: '1px solid var(--chat-bds)',
              borderRadius: 3,
              color: 'var(--chat-subtle)',
            }}
          >
            ↵
          </Code>{' '}
          send ·{' '}
          <Code
            style={{
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              fontSize: 10,
              padding: '1px 4px',
              background: 'var(--chat-surf3)',
              border: '1px solid var(--chat-bds)',
              borderRadius: 3,
              color: 'var(--chat-subtle)',
            }}
          >
            ⇧↵
          </Code>{' '}
          newline · / commands
        </Text>
        <Text
          as="span"
          style={{
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          }}
        >
          42,187 tokens
        </Text>
      </Stack>
    </Stack>
  );
}
