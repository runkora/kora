import * as React from 'react';
import { cn } from '../../utils';

/**
 * ChatBubble + ChatBubbleActions
 *
 * Presentational primitives for rendering chat message bubbles with
 * direction-aware rounding, group/hover interaction, and a reveal-on-hover
 * action bar.
 *
 * **No business logic lives here.** These components know nothing about
 * user roles, message types, AI vs. human, streaming state, or application
 * data models. They take `direction` and `variant` — that is all.
 *
 * ## Usage
 *
 * ```tsx
 * {/* User message — end-aligned, accent variant *\/}
 * <ChatBubble direction="end" variant="accent">
 *   <Text as="span" preserveWhitespace>{message.content}</Text>
 *   <ChatBubbleActions>
 *     <Button variant="ghost" size="icon-sm"><Icon name="copy" /></Button>
 *   </ChatBubbleActions>
 * </ChatBubble>
 *
 * {/* Assistant message — start-aligned, surface variant (default) *\/}
 * <ChatBubble direction="start">
 *   <Text as="span" preserveWhitespace>{message.content}</Text>
 *   <ChatBubbleActions>
 *     <Button variant="ghost" size="icon-sm"><Icon name="copy" /></Button>
 *   </ChatBubbleActions>
 * </ChatBubble>
 * ```
 *
 * ## ChatBubbleActions coupling
 *
 * `ChatBubbleActions` must be rendered **inside** `ChatBubble`. The hover
 * reveal relies on the `group` class placed on the inner bubble div — if
 * `ChatBubbleActions` is rendered outside the bubble, `group-hover` will
 * not fire and the actions will remain invisible.
 *
 * ## className is intentionally blocked
 *
 * Neither `ChatBubble` nor `ChatBubbleActions` accepts a `className` prop.
 * Visual customisation belongs in Hades props. If product code needs a style
 * that is not expressible through these props, that is a signal to add a
 * new prop or variant to Hades — not to reach for className.
 */

// ── Types ─────────────────────────────────────────────────────────────────────

export type ChatBubbleDirection = 'start' | 'end';
export type ChatBubbleVariant = 'surface' | 'accent';

export interface ChatBubbleProps {
  /**
   * Controls alignment and corner rounding.
   *
   * - `'end'`   — right-aligned (user messages), top-right corner flattened.
   * - `'start'` — left-aligned (assistant messages), top-left corner flattened.
   */
  direction: ChatBubbleDirection;

  /**
   * Visual treatment of the bubble.
   *
   * - `'surface'` — neutral surface with a subtle border (default, assistant).
   * - `'accent'`  — tinted accent background (user messages).
   *
   * @default 'surface'
   */
  variant?: ChatBubbleVariant;

  children: React.ReactNode;

  /**
   * className is intentionally not exposed on ChatBubble.
   * Use `direction` and `variant` to control appearance.
   */
  className?: never;
}

export interface ChatBubbleActionsProps {
  children: React.ReactNode;

  /**
   * className is intentionally not exposed on ChatBubbleActions.
   * Must be rendered inside a ChatBubble to receive group-hover signals.
   */
  className?: never;
}

// ── Class maps ────────────────────────────────────────────────────────────────

/** Outer wrapper alignment — full static strings for Tailwind scanner. */
const OUTER_ALIGN: Record<ChatBubbleDirection, string> = {
  end: 'flex flex-col items-end',
  start: 'flex flex-col items-start',
};

/**
 * Inner bubble corner rounding — top corner nearest the edge is flattened
 * to a small radius to visually "anchor" the bubble to its side.
 * Full static strings required for Tailwind v4 scanner.
 */
const BUBBLE_ROUNDING: Record<ChatBubbleDirection, string> = {
  end: 'rounded-2xl rounded-tr-sm',
  start: 'rounded-2xl rounded-tl-sm',
};

/** Bubble background and border by variant. */
const BUBBLE_VARIANT: Record<ChatBubbleVariant, string> = {
  surface: 'bg-surface border border-border-subtle',
  accent: 'bg-accent/10 border border-accent/20',
};

// ── ChatBubble ────────────────────────────────────────────────────────────────

const ChatBubble = React.forwardRef<HTMLDivElement, ChatBubbleProps>(
  ({ direction, variant = 'surface', children }, ref) => (
    <div ref={ref} data-slot="chat-bubble-outer" className={OUTER_ALIGN[direction]}>
      <div
        data-slot="chat-bubble"
        className={cn(
          'group relative max-w-[78%] px-4 py-3',
          BUBBLE_ROUNDING[direction],
          BUBBLE_VARIANT[variant],
        )}
      >
        {children}
      </div>
    </div>
  ),
);
ChatBubble.displayName = 'ChatBubble';

// ── ChatBubbleActions ─────────────────────────────────────────────────────────

/**
 * ChatBubbleActions
 *
 * A reveal-on-hover action bar intended to be placed inside a `ChatBubble`.
 * Uses `group-hover:opacity-100` to respond to the `group` class on the
 * parent bubble — it must be a descendant of `ChatBubble` to work.
 *
 * Renders below the bubble content (flex-col layout from ChatBubble outer).
 */
const ChatBubbleActions = React.forwardRef<HTMLDivElement, ChatBubbleActionsProps>(
  ({ children }, ref) => (
    <div
      ref={ref}
      data-slot="chat-bubble-actions"
      className="mt-1 flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity duration-150"
    >
      {children}
    </div>
  ),
);
ChatBubbleActions.displayName = 'ChatBubbleActions';

// ── Exports ───────────────────────────────────────────────────────────────────

export { ChatBubble, ChatBubbleActions };
