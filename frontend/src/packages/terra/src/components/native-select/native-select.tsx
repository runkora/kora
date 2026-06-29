import * as React from 'react';
import { cn } from '../../utils';
import { useFormField } from '../form-field';

export interface NativeSelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  /** Renders the select in an error state with a coloured border. */
  hasError?: boolean;
  /** Renders the select value in a monospace font. */
  mono?: boolean;
  /**
   * Visual size variant.
   * - `md` (default) — matches Input height (h-9, px-3, text-sm)
   * - `sm` — compact inline use (h-7, px-2, text-xs)
   */
  variant?: 'md' | 'sm';
}

/**
 * NativeSelect
 *
 * A styled wrapper around the native `<select>` element, matched to the same
 * height and appearance as the Terra `Input`. Use when a headless or Radix
 * Select is too heavy — e.g. provider/model dropdowns with `<optgroup>` support.
 *
 * ```tsx
 * <NativeSelect value={provider} onChange={(e) => setProvider(e.target.value)}>
 *   <option value="bedrock">AWS Bedrock</option>
 *   <option value="vertexai">Vertex AI</option>
 * </NativeSelect>
 * ```
 */
const NativeSelect = React.forwardRef<HTMLSelectElement, NativeSelectProps>(
  (
    {
      className,
      hasError = false,
      mono = false,
      variant = 'md',
      children,
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
      <select
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
          'flex w-full rounded-md font-normal text-foreground bg-surface-elevated border',
          // Size variants
          variant === 'sm' ? 'h-7 px-2 py-0 text-xs' : 'h-9 px-3 py-1 text-sm',
          // Border colour — normal vs error
          hasError ? 'border-status-error' : 'border-border',
          // Focus
          'focus-visible:outline-2 focus-visible:outline-offset-2',
          hasError ? 'focus-visible:outline-status-error' : 'focus-visible:outline-accent',
          // Disabled
          'disabled:cursor-not-allowed disabled:opacity-40',
          mono && 'font-mono',
          className,
        )}
        {...props}
      >
        {children}
      </select>
    );
  },
);

NativeSelect.displayName = 'NativeSelect';

export { NativeSelect };
