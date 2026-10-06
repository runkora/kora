'use client';

/** @tier 3 · agents/[slug]-scoped. Renders an agent's markdown instructions in Terra text, with the numbers of ordered lists drawn in React. */

import { Children, createContext, isValidElement, useContext } from 'react';
import type { ReactElement, ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { Components } from 'react-markdown';
import { Card, CardContent, Code, Heading, Stack, Text } from '@orbit/terra';

// The position of the list item being drawn inside an ordered list, 0 outside one.
const ItemNumber = createContext(0);

function P({ children }: { children?: ReactNode }) {
  return <Text size="supporting">{children}</Text>;
}

function Ul({ children }: { children?: ReactNode }) {
  return (
    <Stack gap="tight" align="stretch">
      {children}
    </Stack>
  );
}

function Ol({ children }: { children?: ReactNode }) {
  const items = Children.toArray(children).filter(isValidElement);
  return (
    <Stack gap="tight" align="stretch">
      {items.map((item, i) => (
        <ItemNumber.Provider key={i} value={i + 1}>
          {item}
        </ItemNumber.Provider>
      ))}
    </Stack>
  );
}

function Pre({ children }: { children?: ReactNode }) {
  return <>{children}</>;
}

const BLOCKS: unknown[] = [Ul, Ol, P, Pre];

/** A list item: a marker, its own text, and any nested list or paragraph beneath. */
function Li({ children }: { children?: ReactNode }) {
  const number = useContext(ItemNumber);
  const parts = Children.toArray(children);
  const isBlock = (c: ReactNode) => isValidElement(c) && BLOCKS.includes((c as ReactElement).type);
  const inline = parts.filter((c) => !isBlock(c));
  const blocks = parts.filter(isBlock);
  return (
    <Stack direction="horizontal" align="start" gap="snug">
      <Text size="supporting" color="subtle" tabular>
        {number > 0 ? `${number}.` : '•'}
      </Text>
      <Stack grow minWidth="none" gap="tight" align="stretch">
        {inline.length > 0 && <Text size="supporting">{inline}</Text>}
        {blocks}
      </Stack>
    </Stack>
  );
}

const COMPONENTS: Components = {
  h1: ({ children }) => (
    <Stack paddingTop="snug">
      <Heading variant="label">{children}</Heading>
    </Stack>
  ),
  h2: ({ children }) => (
    <Stack paddingTop="snug">
      <Heading variant="label">{children}</Heading>
    </Stack>
  ),
  h3: ({ children }) => (
    <Stack paddingTop="tight">
      <Text size="supporting" weight="emphasis" color="muted">
        {children}
      </Text>
    </Stack>
  ),
  h4: ({ children }) => (
    <Text size="supporting" weight="emphasis" color="muted">
      {children}
    </Text>
  ),
  p: P,
  strong: ({ children }) => (
    <Text as="span" size="supporting" weight="heavy">
      {children}
    </Text>
  ),
  em: ({ children }) => (
    <Text as="span" size="supporting" style={{ fontStyle: 'italic' }}>
      {children}
    </Text>
  ),
  a: ({ children }) => (
    <Text as="span" size="supporting" color="accent">
      {children}
    </Text>
  ),
  code: ({ children, className }) =>
    className ? (
      <Text mono size="caption" preserveWhitespace style={{ lineHeight: 1.6 }}>
        {children}
      </Text>
    ) : (
      <Code variant={String(children).startsWith('{') ? 'highlight' : 'inline'}>{children}</Code>
    ),
  pre: ({ children }) => (
    <Card subtle>
      <CardContent padded compact>
        <Pre>{children}</Pre>
      </CardContent>
    </Card>
  ),
  ul: Ul,
  ol: Ol,
  li: Li,
  blockquote: ({ children }) => (
    <Stack paddingInline="cozy" gap="snug" style={{ borderLeft: '2px solid var(--color-border)' }}>
      {children}
    </Stack>
  ),
  hr: () => <Stack style={{ borderTop: '1px solid var(--color-border-subtle)' }} />,
};

export function InstructionsMarkdown({ content }: { content: string }) {
  return (
    <Stack gap="snug" align="stretch">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={COMPONENTS}>
        {content}
      </ReactMarkdown>
    </Stack>
  );
}
