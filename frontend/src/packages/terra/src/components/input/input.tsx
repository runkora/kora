import * as React from 'react';
import { cn } from '../../utils';

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  /** Renders the input in an error state with a coloured border. */
  hasError?: boolean;
}

/**
 * Input
 *
 * A single-line text input styled to the Terra token system. Supports all
 * native `<input>` attributes plus an `hasError` flag for validation states.
 *
 * Pair with `Label` and wrap both in a `<div className="flex flex-col gap-1.5">`
 * to build a complete form field.
 *
 * ```tsx
 * <Label htmlFor="agent-name">Agent name</Label>
 * <Input id="agent-name" placeholder="e.g. apollo-3" />
 * ```
 */
const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, hasError = false, type = 'text', ...props }, ref) => {
    return (
      <input
        type={type}
        ref={ref}
        className={cn(
          // Layout & typography
          'flex h-9 w-full rounded-md px-3 py-1',
          'text-sm text-foreground font-normal',
          'bg-surface-elevated border',
          'placeholder:text-subtle',
          // Border colour — normal vs error
          hasError ? 'border-status-error' : 'border-border',
          // Focus
          'focus-visible:outline-2 focus-visible:outline-offset-2',
          hasError ? 'focus-visible:outline-status-error' : 'focus-visible:outline-accent',
          // Disabled
          'disabled:cursor-not-allowed disabled:opacity-40',
          // File input reset
          'file:border-0 file:bg-transparent file:text-sm file:font-medium',
          className,
        )}
        {...props}
      />
    );
  },
);

Input.displayName = 'Input';

export { Input };
