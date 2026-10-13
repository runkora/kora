import * as React from 'react';
import { cn } from '../../utils';
import { useFormField } from '../form-field';

export interface NativeSelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  /** Renders the select in an error state with a coloured border. */
  hasError?: boolean;
  /** Renders the select value in a monospace font. */
  mono?: boolean;
}

// A neutral grey chevron (Fog, 3:1 or more on both light and dark surfaces). It is
// drawn on the select itself so the markup and width behaviour stay the same.
const CHEVRON =
  "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%237f858b' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E\")";

/**
 * NativeSelect
 *
 * A styled wrapper around the native `<select>` element, matched to the same
 * height and appearance as the Hades `Input`. Use when a headless or Radix
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
          // Layout & typography — matches Input height and padding.
          // pr-8 (32px) leaves room for the 16px chevron drawn 12px from the edge.
          'flex h-9 w-full appearance-none rounded-md pl-3 pr-8 py-1',
          'bg-no-repeat bg-[position:right_0.75rem_center] bg-[size:1rem]',
          'text-sm text-foreground font-normal',
          'bg-surface-elevated border',
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
        style={{ backgroundImage: CHEVRON, ...props.style }}
      >
        {children}
      </select>
    );
  },
);

NativeSelect.displayName = 'NativeSelect';

export { NativeSelect };
