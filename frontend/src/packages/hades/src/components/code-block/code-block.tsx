'use client';

import * as React from 'react';
import { useState, useCallback, useEffect, useRef } from 'react';
import { cn } from '../../utils';
import { Button } from '../button';
import { Icon } from '../icon';
import { Code } from '../typography';
import { Text } from '../typography';

// ── Props ─────────────────────────────────────────────────────────────────────

export interface CodeBlockProps {
  /** The code string to display and copy. */
  text: string;
  /** Optional file path shown as a header above the block (e.g. `~/.zshrc`). */
  filename?: string;
  /**
   * Soft-wrap long lines instead of scrolling horizontally. Use for prose-like
   * content (system prompts, descriptions) where horizontal scrolling reads
   * poorly. Preserves newlines, wraps at the block width, and breaks overlong
   * unbreakable tokens so the block never forces the page wider than its column.
   * Defaults to `false` (horizontal scroll — best for real code).
   */
  wrap?: boolean;
  /**
   * Visual density.
   *
   * - `'default'` — 14px on the elevated surface with generous padding; a code
   *   sample that is the point of the page.
   * - `'compact'` — 11px on the base surface with tight padding and a subtle
   *   border; an inset data excerpt inside a row or panel (a webhook payload,
   *   a captured request), where the code is evidence rather than subject.
   *   The copy button appears on hover so the excerpt reads as data at rest.
   */
  density?: 'default' | 'compact';
  /**
   * When `'json'`, tokenises the text and colours keys, values and literals so
   * a payload reads at a glance. Anything that fails to tokenise renders as
   * plain text; highlighting is presentation, never a validator.
   */
  language?: 'json';
  /** @internal Used for internal Hades composition via cn(). Blocked by ESLint in product code. */
  className?: string;
}

// ── JSON highlighting ─────────────────────────────────────────────────────────

/**
 * JSON is split into tokens in one pass, then each token is rendered with the
 * class for its kind: keys carry the accent, string and number values the
 * healthy green, true/false/null the amber literal tone, and everything else
 * (punctuation, whitespace) recedes to subtle, so the eye lands on data.
 *
 * Strings are scanned by hand rather than by regex. A global regex restarts
 * from every quote inside an unterminated string, which is quadratic on long
 * escaped runs; this tokenizer reads each character once and renders an
 * unterminated string as plain text.
 */
type JsonTokenKind = 'key' | 'value' | 'literal' | 'plain';

interface JsonToken {
  kind: JsonTokenKind;
  text: string;
}

const JSON_TOKEN_CLASS: Record<JsonTokenKind, string> = {
  key: 'text-accent',
  value: 'text-status-healthy',
  literal: 'text-status-degraded',
  plain: 'text-subtle',
};

const JSON_LITERALS = ['true', 'false', 'null'];

function isWordChar(ch: string | undefined): boolean {
  return ch !== undefined && /\w/.test(ch);
}

function isDigit(ch: string | undefined): boolean {
  return ch !== undefined && ch >= '0' && ch <= '9';
}

/** Index just past the closing quote of the string opening at `start`, or -1 if unterminated. */
function findStringEnd(src: string, start: number): number {
  let i = start + 1;
  while (i < src.length) {
    if (src[i] === '\\') {
      i += 2;
    } else if (src[i] === '"') {
      return i + 1;
    } else {
      i += 1;
    }
  }
  return -1;
}

/** Index of the first non-whitespace character at or after `start`. */
function skipWhitespace(src: string, start: number): number {
  let i = start;
  while (i < src.length && /\s/.test(src.charAt(i))) i += 1;
  return i;
}

/** Length of the literal (true/false/null) starting at `start`, or 0 if there is none. */
function literalLengthAt(src: string, start: number): number {
  if (isWordChar(src[start - 1])) return 0;
  for (const literal of JSON_LITERALS) {
    if (src.startsWith(literal, start) && !isWordChar(src[start + literal.length])) {
      return literal.length;
    }
  }
  return 0;
}

/** Length of the number (optional sign, digits, optional fraction) starting at `start`, or 0. */
function numberLengthAt(src: string, start: number): number {
  let i = start;
  if (src[i] === '-') i += 1;
  if (!isDigit(src[i])) return 0;
  while (isDigit(src[i])) i += 1;
  if (src[i] === '.' && isDigit(src[i + 1])) {
    i += 1;
    while (isDigit(src[i])) i += 1;
  }
  return i - start;
}

function tokenizeJson(src: string): JsonToken[] {
  const tokens: JsonToken[] = [];
  let plainStart = 0;
  let i = 0;

  const pushPlainUpTo = (end: number) => {
    if (end > plainStart) tokens.push({ kind: 'plain', text: src.slice(plainStart, end) });
  };
  const pushToken = (kind: JsonTokenKind, end: number) => {
    pushPlainUpTo(i);
    tokens.push({ kind, text: src.slice(i, end) });
    i = end;
    plainStart = end;
  };

  while (i < src.length) {
    if (src[i] === '"') {
      const stringEnd = findStringEnd(src, i);
      if (stringEnd === -1) break;
      const afterWhitespace = skipWhitespace(src, stringEnd);
      if (src[afterWhitespace] === ':') {
        pushToken('key', afterWhitespace + 1);
      } else {
        pushToken('value', stringEnd);
      }
      continue;
    }

    const literalLength = literalLengthAt(src, i);
    if (literalLength > 0) {
      pushToken('literal', i + literalLength);
      continue;
    }

    const numberLength = numberLengthAt(src, i);
    if (numberLength > 0) {
      pushToken('value', i + numberLength);
      continue;
    }

    i += 1;
  }

  pushPlainUpTo(src.length);
  return tokens;
}

function highlightJson(src: string): React.ReactNode[] {
  return tokenizeJson(src).map((token, index) => (
    <span key={index} className={JSON_TOKEN_CLASS[token.kind]}>
      {token.text}
    </span>
  ));
}

// ── CodeBlock ─────────────────────────────────────────────────────────────────

/**
 * CodeBlock
 *
 * A multi-line code surface with a one-click copy button. Accepts an optional
 * `filename` prop that renders a labelled header bar above the block, visually
 * merged via shared border and split corner radii.
 *
 * ```tsx
 * <CodeBlock text={`export KORA_API_KEY="sk-kora-xxx"`} filename="~/.zshrc" />
 * ```
 */
export const CodeBlock = React.forwardRef<HTMLElement, CodeBlockProps>(
  ({ text, filename, wrap = false, density = 'default', language, className }, ref) => {
    const [copied, setCopied] = useState(false);
    const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(
      () => () => {
        if (timerRef.current) clearTimeout(timerRef.current);
      },
      [],
    );

    const handleCopy = useCallback(() => {
      void navigator.clipboard.writeText(text).then(() => {
        setCopied(true);
        if (timerRef.current) clearTimeout(timerRef.current);
        timerRef.current = setTimeout(() => setCopied(false), 2000);
      });
    }, [text]);

    return (
      <figure ref={ref as React.Ref<HTMLElement>} className={cn('group relative m-0', className)}>
        {filename ? (
          <figcaption className="flex items-center gap-1.5 rounded-t-lg border border-b-0 border-border bg-surface-overlay px-4 py-2">
            <Icon name="file-text" size="2xs" color="muted" />
            <Text as="span" size="caption" color="subtle">
              {filename}
            </Text>
          </figcaption>
        ) : null}
        <div className="relative">
          <pre
            className={cn(
              'overflow-x-auto border',
              density === 'compact'
                ? 'border-border-subtle bg-surface px-3 py-[11px] leading-normal'
                : 'border-border bg-surface-elevated px-4 py-3 leading-relaxed',
              // Wrap mode: preserve newlines but soft-wrap long lines and break
              // overlong tokens so the block stays within its column width.
              wrap && 'whitespace-pre-wrap break-words',
              filename ? 'rounded-b-lg' : density === 'compact' ? 'rounded-sm' : 'rounded-lg',
            )}
          >
            <Code variant="bare" className={density === 'compact' ? 'text-[11px]' : undefined}>
              {language === 'json' ? highlightJson(text) : text}
            </Code>
          </pre>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={handleCopy}
            aria-label="Copy code"
            className={cn(
              'absolute right-2 top-2',
              density === 'compact' &&
                'opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100',
            )}
          >
            {copied ? (
              <Icon name="check" size="2xs" color="status-healthy" />
            ) : (
              <Icon name="copy" size="2xs" />
            )}
          </Button>
        </div>
      </figure>
    );
  },
);

CodeBlock.displayName = 'CodeBlock';
