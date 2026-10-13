import * as React from 'react';

// ── InputSurface ──────────────────────────────────────────────────────────────

/**
 * InputSurface
 *
 * A styled container for the chat input. Has a rounded border that highlights
 * to `border-accent` when any descendant receives focus.
 *
 * ```tsx
 * <InputSurface>
 *   <Textarea variant="bare" … />
 *   <footer>…</footer>
 * </InputSurface>
 * ```
 */
export const InputSurface = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, children, ...props }, ref) => (
    <div
      ref={ref}
      className={[
        'rounded-2xl border border-border bg-surface transition-colors duration-150 focus-within:border-accent',
        className,
      ]
        .filter(Boolean)
        .join(' ')}
      {...props}
    >
      {children}
    </div>
  ),
);
InputSurface.displayName = 'InputSurface';

// ── TypingIndicator ───────────────────────────────────────────────────────────

/**
 * TypingIndicator
 *
 * Three animated dots shown while the AI is generating a response.
 *
 * ```tsx
 * {isThinking && <ChatBubble direction="start"><TypingIndicator /></ChatBubble>}
 * ```
 */
export const TypingIndicator: React.FC = () => (
  <span className="inline-flex items-center gap-1">
    {[0, 1, 2].map((i) => (
      <span
        key={i}
        className="size-1.5 animate-bounce rounded-full bg-muted"
        style={{ animationDelay: `${i * 150}ms`, animationDuration: '900ms' }}
      />
    ))}
  </span>
);
TypingIndicator.displayName = 'TypingIndicator';

// ── StreamingCursor ───────────────────────────────────────────────────────────

/**
 * StreamingCursor
 *
 * A blinking cursor shown at the end of a streaming message.
 *
 * ```tsx
 * {msg.isStreaming && msg.content && <StreamingCursor />}
 * ```
 */
export const StreamingCursor: React.FC = () => (
  <span className="ml-0.5 inline-block h-3.5 w-0.5 animate-pulse bg-accent align-middle" />
);
StreamingCursor.displayName = 'StreamingCursor';
