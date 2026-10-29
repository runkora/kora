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
  /**
   * Content pinned inside the input's left edge — a currency sign, protocol
   * prefix, or similar. Rendered in a non-interactive subtle mono span; the
   * input text is inset (`pl-7`) to clear it.
   *
   * Named `startAdornment` (not `prefix`) because `prefix` is already a
   * string-typed HTML attribute on inputs.
   *
   * ```tsx
   * <Input type="number" startAdornment="$" endAdornment="/ 1M tokens" />
   * ```
   */
  startAdornment?: React.ReactNode;
  /**
   * `inline` drops the box so the input reads as a word in a sentence: no
   * border at rest, a soft fill on hover, a bordered field on focus; `filled` adds the fill at rest.
   * Use it inside sentence-style builders, next to `InlineToken`.
   */
  variant?: 'default' | 'inline';
  /** `accent` colours the text for values the user has chosen. Only applies to `variant="inline"`. */
  tone?: 'default' | 'accent';
  /**
   * Fills the input at rest. `default` (inline variant only) uses the soft overlay fill, so an
   * empty field still shows where it is, and strengthens it to the accent fill on hover. `danger` uses the error status tint, on any
   * variant, to mark a field that failed validation.
   */
  filled?: 'default' | 'danger';
  /**
   * Content pinned inside the input's right edge — a unit label such as
   * `/ 1M tokens`. Rendered in a non-interactive subtle mono span; the input
   * text is inset (`pr-16`) to clear it.
   */
  endAdornment?: React.ReactNode;
}

/**
 * Input
 *
 * A single-line text input styled to the Hades token system. Supports all
 * native `<input>` attributes plus `hasError` for validation states, `mono`
 * for monospace rendering of technical values, and `startAdornment` /
 * `endAdornment` for in-field prefix/suffix decorations (currency signs,
 * units).
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
 *
 * // Pricing field with currency and unit adornments:
 * <Input type="number" startAdornment="$" endAdornment="/ 1M tokens" />
 * ```
 */
const Input = React.forwardRef<HTMLInputElement, InputProps>(
  (
    {
      className,
      hasError = false,
      mono = false,
      compact = false,
      variant = 'default',
      tone = 'default',
      filled,
      startAdornment,
      endAdornment,
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
    const hasAdornment = startAdornment != null || endAdornment != null;
    const input = (
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
          variant === 'inline'
            ? 'flex w-full rounded-md px-1.5 py-0.5'
            : compact
              ? 'flex h-8 w-full rounded-md px-3'
              : 'flex h-9 w-full rounded-md px-3 py-1',
          mono ? 'text-sm font-mono' : 'text-sm font-normal',
          variant === 'inline' && tone === 'accent' ? 'text-accent' : 'text-foreground',
          variant === 'inline'
            ? 'border bg-transparent transition-colors hover:bg-surface-overlay focus-visible:bg-surface'
            : 'bg-surface border',
          variant === 'inline' &&
            filled === 'default' && [
              'bg-surface-overlay enabled:hover:bg-accent enabled:hover:text-accent-foreground enabled:hover:placeholder:text-accent-foreground',
              // Focused, the field is a plain editing surface again, so hovering it must not
              // pair the dark engaged text with the dark focus background.
              'enabled:focus-visible:hover:bg-surface enabled:focus-visible:hover:placeholder:text-subtle',
              tone === 'accent'
                ? 'enabled:focus-visible:hover:text-accent'
                : 'enabled:focus-visible:hover:text-foreground',
            ],
          filled === 'danger' && 'bg-status-error/10 hover:bg-status-error/15',
          'placeholder:text-subtle',
          // Border colour — normal vs error
          hasError
            ? 'border-status-error'
            : variant === 'inline'
              ? 'border-transparent focus-visible:border-accent'
              : 'border-border',
          // Focus
          'focus-visible:outline-2 focus-visible:outline-offset-2',
          hasError ? 'focus-visible:outline-status-error' : 'focus-visible:outline-accent',
          // Disabled
          'disabled:cursor-not-allowed disabled:opacity-40',
          // File input reset
          'file:border-0 file:bg-transparent file:text-sm file:font-medium',
          // Adornment insets — only emitted when the wrapper renders the spans
          startAdornment != null && 'pl-7',
          endAdornment != null && 'pr-16',
          className,
        )}
        {...props}
      />
    );

    if (!hasAdornment) return input;

    return (
      <div data-slot="input-adorned" className="relative">
        {startAdornment != null && (
          <span
            data-slot="input-start-adornment"
            className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-xs font-mono text-subtle"
          >
            {startAdornment}
          </span>
        )}
        {input}
        {endAdornment != null && (
          <span
            data-slot="input-end-adornment"
            className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs font-mono text-subtle"
          >
            {endAdornment}
          </span>
        )}
      </div>
    );
  },
);

Input.displayName = 'Input';

export { Input };
