'use client';

/** @tier 3 · agents/[slug]-scoped. Renders an agent's markdown instructions in Terra text, with the numbers of ordered lists drawn in React. */

import { Children, createContext, isValidElement, useContext } from 'react';
import type { ElementType, ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { Components } from 'react-markdown';
import {
  Anchor,
  Card,
  CardContent,
  Code,
  Heading,
  List,
  ListItem,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';

/** Line height of preformatted text, shared by the Raw view and code blocks. */
export const CODE_LINE_HEIGHT = 1.6;

// The position of the list item being drawn inside an ordered list, 0 outside one.
const ItemNumber = createContext(0);

interface MdNode {
  type: string;
  value?: string;
  children?: MdNode[];
  data?: { hName: string };
}

const VARIABLE = /\{\{?\s*[\w.-]+\s*\}?\}/g;

/** Splits `{{name}}` and `{name}` in text nodes into `mark` nodes, leaving code and link destinations alone. */
function markVariables(node: MdNode): void {
  if (!node.children || node.type === 'code' || node.type === 'inlineCode') return;
  node.children = node.children.flatMap((child) => {
    if (child.type !== 'text' || !child.value) {
      markVariables(child);
      return [child];
    }
    const out: MdNode[] = [];
    let last = 0;
    for (const m of child.value.matchAll(VARIABLE)) {
      if (m.index > last) out.push({ type: 'text', value: child.value.slice(last, m.index) });
      out.push({
        type: 'variable',
        data: { hName: 'mark' },
        children: [{ type: 'text', value: m[0] }],
      });
      last = m.index + m[0].length;
    }
    if (last === 0) return [child];
    if (last < child.value.length) out.push({ type: 'text', value: child.value.slice(last) });
    return out;
  });
}

const remarkVariables = () => markVariables;

/** The text of a rendered node, for a code block whose lines must keep their whitespace. */
function textOf(node: ReactNode): string {
  if (typeof node === 'string') return node;
  if (Array.isArray(node)) return node.map(textOf).join('');
  return isValidElement<{ children?: ReactNode }>(node) ? textOf(node.props.children) : '';
}

function P({ children }: { children?: ReactNode }) {
  return <Text size="supporting">{children}</Text>;
}

function Ul({ children }: { children?: ReactNode }) {
  return (
    <ItemNumber.Provider value={0}>
      <List gap="tight">{children}</List>
    </ItemNumber.Provider>
  );
}

function Ol({ children }: { children?: ReactNode }) {
  const items = Children.toArray(children).filter(isValidElement);
  return (
    <List ordered gap="tight">
      {items.map((item, i) => (
        <ItemNumber.Provider key={i} value={i + 1}>
          {item}
        </ItemNumber.Provider>
      ))}
    </List>
  );
}

function CodeBlock({ children }: { children?: ReactNode }) {
  return (
    <Card subtle>
      <CardContent padded compact>
        <Text mono size="caption" preserveWhitespace style={{ lineHeight: CODE_LINE_HEIGHT }}>
          {textOf(children).replace(/\n$/, '')}
        </Text>
      </CardContent>
    </Card>
  );
}

function Quote({ children }: { children?: ReactNode }) {
  return (
    <Stack paddingInline="cozy" gap="snug" style={{ borderLeft: '2px solid var(--color-border)' }}>
      {children}
    </Stack>
  );
}

function Rule() {
  return <Separator />;
}

function H1({ children }: { children?: ReactNode }) {
  return (
    <Stack paddingTop="snug">
      <Heading variant="label">{children}</Heading>
    </Stack>
  );
}

function H3({ children }: { children?: ReactNode }) {
  return (
    <Stack paddingTop="tight">
      <Text size="supporting" weight="emphasis" color="muted">
        {children}
      </Text>
    </Stack>
  );
}

function H4({ children }: { children?: ReactNode }) {
  return (
    <Text size="supporting" weight="emphasis" color="muted">
      {children}
    </Text>
  );
}

// Block renderers: inside a list item they sit beside its text, never inside the Text paragraph.
const BLOCKS: ElementType[] = [Ul, Ol, P, CodeBlock, Quote, Rule, H1, H3, H4];

/** A list item: a marker, its own text, and any nested block beneath. */
function Li({ children }: { children?: ReactNode }) {
  const number = useContext(ItemNumber);
  const parts = Children.toArray(children);
  const isBlock = (c: ReactNode) => isValidElement(c) && BLOCKS.includes(c.type as ElementType);
  const inline = parts.filter((c) => !isBlock(c));
  const blocks = parts.filter(isBlock);
  return (
    <ListItem>
      <Stack direction="horizontal" align="start" gap="snug">
        <Text size="supporting" color="subtle" tabular>
          {number > 0 ? `${number}.` : '•'}
        </Text>
        <Stack grow minWidth="none" gap="tight" align="stretch">
          {inline.length > 0 && <Text size="supporting">{inline}</Text>}
          {blocks}
        </Stack>
      </Stack>
    </ListItem>
  );
}

const COMPONENTS: Components = {
  h1: H1,
  h2: H1,
  h3: H3,
  h4: H4,
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
  a: ({ children, href, title }) => (
    <Anchor href={href} title={title} target="_blank" rel="noopener noreferrer">
      {children}
    </Anchor>
  ),
  mark: ({ children }) => <Code variant="highlight">{children}</Code>,
  code: ({ children }) => <Code variant="inline">{children}</Code>,
  pre: CodeBlock,
  ul: Ul,
  ol: Ol,
  li: Li,
  blockquote: Quote,
  hr: Rule,
};

export function InstructionsMarkdown({ content }: { content: string }) {
  return (
    <Stack gap="snug" align="stretch">
      <ReactMarkdown remarkPlugins={[remarkGfm, remarkVariables]} components={COMPONENTS}>
        {content}
      </ReactMarkdown>
    </Stack>
  );
}
