/** @tier 3 — mcp-gateway-scoped. Syntax-coloured config block (JSON or CLI) with copy. */
'use client';

import { useState } from 'react';
import { Button, Icon, Stack, Text } from '@orbit/terra';

// Token colours taken verbatim from the design spec.
const TONE = {
  key: 'var(--color-accent)',
  str: '#9ce37d',
  literal: 'var(--color-status-degraded)',
  subtle: 'var(--color-subtle)',
  foreground: 'var(--color-foreground)',
} as const;

interface Token {
  text: string;
  tone: string;
}

/** Tokenises the (Orbit-generated, well-formed) config JSON for colouring. */
function tokenizeJson(json: string): Token[] {
  const re =
    /("(?:[^"\\]|\\.)*")(\s*:)?|(-?\d+(?:\.\d+)?|true|false|null)|([{}[\],:])|(\s+)|([^\s{}[\],:"]+)/g;
  const out: Token[] = [];
  let m: RegExpExecArray | null;
  while ((m = re.exec(json)) !== null) {
    if (m[1] !== undefined) {
      if (m[2] !== undefined) {
        out.push({ text: m[1], tone: TONE.key });
        out.push({ text: m[2], tone: TONE.subtle });
      } else {
        out.push({ text: m[1], tone: TONE.str });
      }
    } else if (m[3] !== undefined) {
      out.push({ text: m[3], tone: /^(true|false|null)$/.test(m[3]) ? TONE.literal : TONE.str });
    } else {
      out.push({ text: m[0], tone: TONE.subtle });
    }
  }
  return out;
}

/** Colours each `claude mcp add …` command line. */
function tokenizeCommands(text: string): Token[][] {
  return text.split('\n').map((line) => {
    const m = line.match(/^(claude mcp add )(--transport http )(\S+ )(\S+)$/);
    if (!m) return [{ text: line, tone: TONE.foreground }];
    return [
      { text: '$ ', tone: TONE.subtle },
      { text: m[1], tone: TONE.foreground },
      { text: m[2], tone: TONE.literal },
      { text: m[3], tone: TONE.key },
      { text: m[4], tone: TONE.str },
    ];
  });
}

interface ConfigBlockProps {
  text: string;
  filename?: string;
  /** Render as coloured shell commands (Claude Code) instead of JSON. */
  command?: boolean;
}

export function ConfigBlock({ text, filename, command = false }: ConfigBlockProps) {
  const [copied, setCopied] = useState(false);

  const copy = () => {
    void navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  const commandLines = command ? tokenizeCommands(text) : null;
  const jsonTokens = command ? null : tokenizeJson(text);

  return (
    <Stack
      gap="none"
      style={{
        border: '1px solid var(--color-border)',
        borderRadius: 'var(--radius-lg)',
        overflow: 'hidden',
        background: 'var(--color-surface)',
      }}
    >
      {filename ? (
        <Stack
          direction="horizontal"
          align="center"
          gap="tight"
          paddingInline="snug"
          paddingBlock="tight"
          style={{
            borderBottom: '1px solid var(--color-border-subtle)',
            background: 'var(--color-surface-elevated)',
          }}
        >
          <Icon name="file-text" size="2xs" color="muted" />
          <Text mono size="caption" color="muted">
            {filename}
          </Text>
        </Stack>
      ) : null}

      <Stack style={{ position: 'relative' }}>
        <Button
          variant="outline"
          size="sm"
          onClick={copy}
          style={{
            position: 'absolute',
            top: 9,
            right: 9,
            zIndex: 1,
            height: 28,
            padding: '0 10px',
            borderRadius: 6,
            fontSize: 11.5,
            ...(copied
              ? {
                  background: 'var(--color-accent-muted)',
                  borderColor: 'var(--color-accent-border)',
                  color: 'var(--color-accent)',
                }
              : {}),
          }}
        >
          <Icon name={copied ? 'check' : 'copy'} size="2xs" />
          {copied ? 'Copied' : 'Copy'}
        </Button>
        <Stack
          style={{
            display: 'block',
            whiteSpace: command ? 'pre-wrap' : 'pre',
            wordBreak: command ? 'break-all' : undefined,
            overflowX: 'auto',
            padding: '14px 16px',
            lineHeight: 1.65,
          }}
        >
          {command
            ? commandLines!.map((line, li) => (
                <Stack
                  key={li}
                  style={{ display: 'block', paddingBottom: li < commandLines!.length - 1 ? 6 : 0 }}
                >
                  {line.map((tk, i) => (
                    <Text as="span" key={i} mono size="caption" style={{ color: tk.tone }}>
                      {tk.text}
                    </Text>
                  ))}
                </Stack>
              ))
            : jsonTokens!.map((tk, i) => (
                <Text as="span" key={i} mono size="caption" style={{ color: tk.tone }}>
                  {tk.text}
                </Text>
              ))}
        </Stack>
      </Stack>
    </Stack>
  );
}
