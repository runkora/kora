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
  /** @internal Used for internal Terra composition via cn(). Blocked by ESLint in product code. */
  className?: string;
}

// ── JSON highlighting ─────────────────────────────────────────────────────────

/**
 * Single left-to-right pass, four token kinds. Strings are scanned by hand
 * rather than by regex: a global regex restarts from every quote inside an
 * unterminated string, which is quadratic on long escaped runs, whereas this
 * scanner visits each character once and treats an unterminated string as
 * plain text. A string is a key when a colon follows it. Keys carry the
 * accent, values carry the healthy green, true/false/null the amber literal
 * tone, and everything else (punctuation, whitespace) recedes to subtle, so
 * the eye lands on data.
 */
const LITERAL_AT = /true|false|null/y;
const NUMBER_AT = /-?\d+(?:\.\d+)?/y;
const KEY_COLON_AT = /\s*:/y;

/** Index just past the closing quote of the string opening at `from`, or -1. */
function scanString(src: string, from: number): number {
  for (let i = from + 1; i < src.length; i++) {
    const ch = src[i];
    if (ch === '\\') {
      i++;
      continue;
    }
    if (ch === '"') return i + 1;
  }
  return -1;
}

function isWordChar(ch: string | undefined): boolean {
  return ch !== undefined && /\w/.test(ch);
}

function highlightJson(src: string): React.ReactNode[] {
  const parts: React.ReactNode[] = [];
  let last = 0;
  let key = 0;
  const flushPlain = (upTo: number) => {
    if (upTo > last) {
      parts.push(
        <span key={key++} className="text-subtle">
          {src.slice(last, upTo)}
        </span>,
      );
    }
  };
  const pushToken = (from: number, to: number, cls: string) => {
    flushPlain(from);
    parts.push(
      <span key={key++} className={cls}>
        {src.slice(from, to)}
      </span>,
    );
    last = to;
  };

  let i = 0;
  while (i < src.length) {
    const ch = src[i];
    if (ch === '"') {
      const close = scanString(src, i);
      if (close === -1) break;
      KEY_COLON_AT.lastIndex = close;
      const colon = KEY_COLON_AT.exec(src);
      if (colon) {
        pushToken(i, close + colon[0].length, 'text-accent');
      } else {
        pushToken(i, close, 'text-status-healthy');
      }
      i = last;
      continue;
    }
    if ((ch === 't' || ch === 'f' || ch === 'n') && !isWordChar(src[i - 1])) {
      LITERAL_AT.lastIndex = i;
      const lit = LITERAL_AT.exec(src);
      if (lit && !isWordChar(src[i + lit[0].length])) {
        pushToken(i, i + lit[0].length, 'text-status-degraded');
        i = last;
        continue;
      }
    }
    if (ch === '-' || (ch >= '0' && ch <= '9')) {
      NUMBER_AT.lastIndex = i;
      const num = NUMBER_AT.exec(src);
      if (num) {
        pushToken(i, i + num[0].length, 'text-status-healthy');
        i = last;
        continue;
      }
    }
    i++;
  }
  flushPlain(src.length);
  last = src.length;
  return parts;
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
 * <CodeBlock text={`export ORBIT_API_KEY="sk-orbit-xxx"`} filename="~/.zshrc" />
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
