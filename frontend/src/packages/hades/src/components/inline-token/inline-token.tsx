import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';
import { Icon } from '../icon';

/** @tier 1 — Zero kora domain knowledge. A word inside a sentence that opens a picker. */

const inlineTokenVariants = cva(
  [
    'inline-flex max-w-full items-center gap-1 rounded-md px-1.5 py-0.5 text-sm',
    'bg-transparent transition-colors',
    'focus-visible:outline-2 focus-visible:outline-offset-0 focus-visible:outline-accent',
    'disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent',
  ],
  {
    variants: {
      tone: {
        foreground: 'text-foreground',
        subtle: 'text-subtle',
      },
      mono: { true: 'font-mono', false: '' },
      // A value that failed validation: the error tint at rest, a step stronger on hover.
      invalid: { true: 'bg-status-error/10 hover:bg-status-error/15', false: '' },
      // The fill appears only while hovered, focused or open.
      fill: {
        true: 'hover:bg-surface-overlay focus-visible:bg-surface-overlay data-[state=open]:bg-surface-overlay',
        false: '',
      },
    },
    defaultVariants: { tone: 'foreground', mono: false, fill: true, invalid: false },
  },
);

export interface InlineTokenProps
  extends
    Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, 'className'>,
    VariantProps<typeof inlineTokenVariants> {
  /** Fills on hover, focus and open. Pass false when something else answers the hover. Defaults to true. */
  fill?: boolean | undefined;
  /** Marks the token as holding a value that failed validation. */
  invalid?: boolean | undefined;
  /** Shows the open/close chevron after the label. Defaults to true. */
  chevron?: boolean | undefined;
  /** Whether the picker this token opens is open, to flip the chevron. */
  open?: boolean | undefined;
  className?: never;
}

/**
 * InlineToken
 *
 * A button that reads as a word in a sentence: no fill or border at rest, a
 * soft fill on hover, focus or while its picker is open. Use it as the trigger
 * of a `Popover`, for the fields of a sentence-style builder.
 *
 * ```tsx
 * <PopoverTrigger asChild>
 *   <InlineToken mono>pull_request</InlineToken>
 * </PopoverTrigger>
 * ```
 */
const InlineToken = React.forwardRef<HTMLButtonElement, InlineTokenProps>(
  (
    {
      tone,
      mono,
      fill = true,
      invalid = false,
      chevron = true,
      open,
      children,
      type = 'button',
      ...props
    },
    ref,
  ) => (
    <button
      ref={ref}
      type={type}
      data-slot="inline-token"
      className={cn(inlineTokenVariants({ tone, mono: mono ?? false, fill, invalid }))}
      {...props}
    >
      <span className="min-w-0 truncate">{children}</span>
      {chevron && (
        <Icon name={open ? 'chevron-up' : 'chevron-down'} size="2xs" color="muted" shrink={false} />
      )}
    </button>
  ),
);
InlineToken.displayName = 'InlineToken';

export { InlineToken, inlineTokenVariants };
