'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Renders agent markdown prose using react-markdown + remark-gfm with Hades
 * component renderers. Falls back to plain pre-wrap text while streaming.
 */

import { createContext, useContext, useMemo } from 'react';
import type { CSSProperties } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { Components } from 'react-markdown';
import { Text, Code, Stack } from '@orbit/hades';

// ── Contexts shared by nested renderers ──────────────────────────────────────

// true when rendering inside an ordered list (so <li> can emit the counter)
const ListOrderedCtx = createContext(false);
// true when rendering inside a fenced code block (so <code> renders block-style)
const InPreCtx = createContext(false);

// ── Named sub-components (required for hooks rules-of-hooks) ─────────────────

function MdCode({ children }: { children?: React.ReactNode }) {
  const inPre = useContext(InPreCtx);
  if (inPre) {
    return (
      <Code
        style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 12,
          color: 'var(--color-foreground)',
          whiteSpace: 'pre',
          display: 'block',
          lineHeight: 1.55,
        }}
      >
        {children}
      </Code>
    );
  }
  return (
    <Code
      style={{
        fontFamily: 'var(--font-mono)',
        fontSize: 12,
        color: 'var(--color-accent)',
        background: 'color-mix(in oklch, var(--color-accent) 12%, transparent)',
        border: '1px solid var(--color-border-subtle)',
        borderRadius: 3,
        padding: '1px 5px',
      }}
    >
      {children}
    </Code>
  );
}

function MdLi({ children }: { children?: React.ReactNode }) {
  const ordered = useContext(ListOrderedCtx);
  return (
    <Stack direction="horizontal" style={{ gap: 8, alignItems: 'flex-start', marginBottom: 2 }}>
      {ordered ? (
        <Text
          as="span"
          data-md-ol-num=""
          style={{
            flexShrink: 0,
            fontSize: 14,
            color: 'var(--color-subtle)',
            lineHeight: 1.65,
            minWidth: 18,
            marginTop: 1,
            fontVariantNumeric: 'tabular-nums',
          }}
        />
      ) : (
        <Text
          as="span"
          style={{
            flexShrink: 0,
            fontSize: 14,
            color: 'var(--color-accent)',
            lineHeight: 1.65,
            marginTop: 1,
          }}
        >
          •
        </Text>
      )}
      <Text style={{ fontSize: 14, lineHeight: 1.65, flex: 1 }}>{children}</Text>
    </Stack>
  );
}

// ── Hades component map ───────────────────────────────────────────────────────

const HEADING_STYLES: Record<number, CSSProperties> = {
  1: {
    fontSize: 15,
    fontWeight: 650,
    marginTop: 12,
    marginBottom: 4,
    color: 'var(--color-foreground)',
    display: 'block',
  },
  2: {
    fontSize: 14,
    fontWeight: 600,
    marginTop: 8,
    marginBottom: 3,
    color: 'var(--color-foreground)',
    display: 'block',
  },
  3: {
    fontSize: 13,
    fontWeight: 600,
    marginTop: 6,
    marginBottom: 2,
    color: 'var(--color-muted)',
    display: 'block',
  },
};

const MD: Components = {
  h1: ({ children }) => (
    <Text as="span" weight="heavy" style={HEADING_STYLES[1]}>
      {children}
    </Text>
  ),
  h2: ({ children }) => (
    <Text as="span" weight="heavy" style={HEADING_STYLES[2]}>
      {children}
    </Text>
  ),
  h3: ({ children }) => (
    <Text as="span" weight="heavy" style={HEADING_STYLES[3]}>
      {children}
    </Text>
  ),
  h4: ({ children }) => (
    <Text as="span" weight="heavy" style={{ ...HEADING_STYLES[3], fontSize: 12 }}>
      {children}
    </Text>
  ),
  h5: ({ children }) => (
    <Text as="span" style={{ ...HEADING_STYLES[3], fontSize: 11 }}>
      {children}
    </Text>
  ),
  h6: ({ children }) => (
    <Text as="span" style={{ ...HEADING_STYLES[3], fontSize: 11, color: 'var(--color-border)' }}>
      {children}
    </Text>
  ),
  p: ({ children }) => (
    <Text style={{ fontSize: 14, lineHeight: 1.65, marginBottom: 2 }}>{children}</Text>
  ),
  strong: ({ children }) => (
    <Text as="span" weight="heavy">
      {children}
    </Text>
  ),
  em: ({ children }) => (
    <Text as="span" style={{ fontStyle: 'italic' }}>
      {children}
    </Text>
  ),
  code: ({ children }) => <MdCode>{children}</MdCode>,
  pre: ({ children }) => (
    <InPreCtx.Provider value={true}>
      <Stack
        style={{
          background: 'var(--color-surface-elevated)',
          border: '1px solid var(--color-border-subtle)',
          borderRadius: 6,
          padding: '10px 14px',
          marginBottom: 6,
          overflowX: 'auto',
        }}
      >
        {children}
      </Stack>
    </InPreCtx.Provider>
  ),
  ul: ({ children }) => (
    <ListOrderedCtx.Provider value={false}>
      <Stack style={{ gap: 0, marginBottom: 4 }}>{children}</Stack>
    </ListOrderedCtx.Provider>
  ),
  ol: ({ children }) => (
    <ListOrderedCtx.Provider value={true}>
      <Stack data-md-ol="" style={{ gap: 0, marginBottom: 4 }}>
        {children}
      </Stack>
    </ListOrderedCtx.Provider>
  ),
  li: ({ children }) => <MdLi>{children}</MdLi>,
  hr: () => (
    <Stack
      style={{ height: 1, background: 'var(--color-border-subtle)', opacity: 0.4, margin: '8px 0' }}
    />
  ),
  blockquote: ({ children }) => (
    <Stack
      style={{
        borderLeft: '3px solid var(--color-border)',
        paddingLeft: 12,
        marginBottom: 6,
        color: 'var(--color-muted)',
      }}
    >
      {children}
    </Stack>
  ),
  table: ({ children }) => (
    <Stack style={{ overflowX: 'auto', marginBottom: 8 }}>
      <Stack
        style={{
          border: '1px solid var(--color-border-subtle)',
          borderRadius: 6,
          overflow: 'hidden',
          width: 'fit-content',
        }}
      >
        {children}
      </Stack>
    </Stack>
  ),
  thead: ({ children }) => (
    <Stack style={{ background: 'var(--color-surface-elevated)' }}>{children}</Stack>
  ),
  tbody: ({ children }) => <Stack>{children}</Stack>,
  tr: ({ children }) => (
    <Stack direction="horizontal" style={{ borderBottom: '1px solid var(--color-border-subtle)' }}>
      {children}
    </Stack>
  ),
  th: ({ children }) => (
    <Text
      as="span"
      weight="heavy"
      style={{
        fontSize: 12,
        padding: '6px 12px',
        color: 'var(--color-foreground)',
        borderRight: '1px solid var(--color-border-subtle)',
        minWidth: 80,
        display: 'block',
        whiteSpace: 'nowrap',
      }}
    >
      {children}
    </Text>
  ),
  td: ({ children }) => (
    <Text
      as="span"
      style={{
        fontSize: 12,
        padding: '6px 12px',
        color: 'var(--color-muted)',
        borderRight: '1px solid var(--color-border-subtle)',
        minWidth: 80,
        display: 'block',
        lineHeight: 1.55,
      }}
    >
      {children}
    </Text>
  ),
  a: ({ href, children }) => (
    <Text
      as="span"
      style={{
        color: 'var(--color-accent)',
        textDecoration: 'underline',
        textUnderlineOffset: 3,
        cursor: 'pointer',
      }}
      onClick={() => href && window.open(href, '_blank', 'noopener,noreferrer')}
    >
      {children}
    </Text>
  ),
};

// ── Public component ──────────────────────────────────────────────────────────

export function AgentTextBlock({
  content,
  isStreaming = false,
}: {
  content: string;
  isStreaming?: boolean;
}) {
  // Skip parsing while streaming — render as plain pre-wrap text.
  // A single parse runs once isStreaming flips to false.
  const shouldParse = useMemo(() => !isStreaming, [isStreaming]);

  if (!shouldParse) {
    return (
      <Text
        style={{
          fontSize: 14,
          lineHeight: 1.65,
          color: 'var(--color-foreground)',
          whiteSpace: 'pre-wrap',
        }}
      >
        {content}
      </Text>
    );
  }

  return (
    <Stack style={{ color: 'var(--color-foreground)', gap: 4 }}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={MD}>
        {content}
      </ReactMarkdown>
    </Stack>
  );
}
