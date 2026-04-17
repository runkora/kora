import * as React from 'react';
import { cn } from '../../utils';

export interface NativeSelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  /** Renders the select in an error state with a coloured border. */
  hasError?: boolean;
  /** Renders the select value in a monospace font. */
  mono?: boolean;
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
  ({ className, hasError = false, mono = false, children, ...props }, ref) => {
    return (
      <select
        ref={ref}
        className={cn(
          // Layout & typography — matches Input height and padding
          'flex h-9 w-full rounded-md px-3 py-1',
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
      >
        {children}
      </select>
    );
  },
);

NativeSelect.displayName = 'NativeSelect';

export { NativeSelect };
