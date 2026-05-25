'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Renders agent prose paragraphs with inline `code` backtick spans highlighted
 * in accent colour.
 */

import type { ReactNode } from 'react';
import { Text, Code, Stack } from '@orbit/terra';

// Splits text on backtick pairs and returns a mix of text nodes + <Code> components.
function parseInlineCode(text: string): ReactNode[] {
  const parts = text.split('`');
  return parts.map((part, i) => {
    if (i % 2 === 1) {
      // Odd indices are inside backtick pairs → render as code
      return (
        <Code
          key={i}
          style={{
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            fontSize: 12,
            color: 'var(--chat-acc)',
            background: 'var(--chat-acc-dim)',
            border: '1px solid var(--chat-bds)',
            borderRadius: 3,
            padding: '1px 5px',
          }}
        >
          {part}
        </Code>
      );
    }
    return part;
  });
}

export function AgentTextBlock({ content }: { content: string }) {
  const paragraphs = content.split('\n\n').filter(Boolean);

  return (
    <Stack
      style={{
        color: 'var(--chat-fg)',
        lineHeight: 1.65,
        marginBottom: 10,
      }}
    >
      {paragraphs.map((para, i) => (
        <Text key={i} style={{ marginTop: i > 0 ? 6 : 0 }}>
          {parseInlineCode(para)}
        </Text>
      ))}
    </Stack>
  );
}
