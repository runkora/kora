import * as React from 'react';
import { cn } from '../../utils';
import { useFormField } from '../form-field';

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  /** Renders the input in an error state with a coloured border. */
  hasError?: boolean;
  /**
   * When true, renders the input text in a monospace font.
   * Use for fields whose value is a technical string: API keys, resource paths,
   * URL patterns, regex expressions, and similar machine-readable content.
   *
   * ```tsx
   * <Input mono placeholder="/api/v1/agents/:id" />
   * <Input mono value={apiKey} readOnly />
   * ```
   */
  mono?: boolean;
  /**
   * When true, reduces height from `h-9` (36px) to `h-8` (32px).
   * Use when the input sits adjacent to a compact control (e.g. a native
   * `<select>` with `h-8`) and visual height parity is required.
   */
  compact?: boolean;
}

/**
 * Input
 *
 * A single-line text input styled to the Terra token system. Supports all
 * native `<input>` attributes plus `hasError` for validation states and `mono`
 * for monospace rendering of technical values.
 *
 * Pair with `Label` and wrap both in a `<div className="flex flex-col gap-1.5">`
 * to build a complete form field.
 *
 * ```tsx
 * <Label htmlFor="agent-name">Agent name</Label>
 * <Input id="agent-name" placeholder="e.g. apollo-3" />
 *
 * // Monospace — for API keys, paths, URL patterns:
 * <Input mono placeholder="/api/v1/agents/:id" />
 * ```
 */
const Input = React.forwardRef<HTMLInputElement, InputProps>(
  (
    {
      className,
      hasError = false,
      mono = false,
      compact = false,
      type = 'text',
      id,
      'aria-describedby': ariaDescribedby,
      'aria-invalid': ariaInvalid,
      'aria-required': ariaRequired,
      ...props
    },
    ref,
  ) => {
    const field = useFormField();
    return (
      <input
        type={type}
        ref={ref}
        id={id ?? field?.id}
        aria-describedby={
          ariaDescribedby ??
          (field
            ? [field.hasHint ? field.hintId : '', field.hasError ? field.errorId : '']
                .filter(Boolean)
                .join(' ') || undefined
            : undefined)
        }
        aria-invalid={ariaInvalid ?? field?.hasError ?? undefined}
        aria-required={ariaRequired ?? field?.required ?? undefined}
        className={cn(
          // Layout & typography
          compact ? 'flex h-8 w-full rounded-md px-3' : 'flex h-9 w-full rounded-md px-3 py-1',
          mono ? 'text-sm text-foreground font-mono' : 'text-sm text-foreground font-normal',
          'bg-surface border',
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
