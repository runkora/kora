import * as React from 'react';
import { cn } from '../../utils';

export interface ToggleProps {
  /** The controlled checked state. */
  checked: boolean;
  /** Called with the new state when the user clicks the toggle. */
  onChange: (checked: boolean) => void;
  /** Accessible name for screen readers. Required when there is no adjacent visible label. */
  'aria-label'?: string;
  /** Associates the toggle with a visible label element by ID. */
  'aria-labelledby'?: string;
  /** Prevents interaction and dims the toggle. */
  disabled?: boolean;
  className?: string;
}

/**
 * Toggle
 *
 * An accessible on/off switch. Pair with a visible label or supply `aria-label`
 * so screen readers can announce what is being toggled.
 *
 * ```tsx
 * <Toggle checked={enabled} onChange={setEnabled} aria-label="Enable model" />
 * ```
 */
const Toggle = React.forwardRef<HTMLButtonElement, ToggleProps>(
  ({ checked, onChange, disabled = false, className, ...ariaProps }, ref) => {
    return (
      <button
        ref={ref}
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => !disabled && onChange(!checked)}
        className={cn(
          'relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent',
          'transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
          checked ? 'bg-accent' : 'bg-surface-elevated',
          disabled && 'cursor-not-allowed opacity-40',
          className,
        )}
        {...ariaProps}
      >
        <span
          className={cn(
            'pointer-events-none inline-block h-4 w-4 rounded-full bg-foreground shadow transition-transform',
            checked ? 'translate-x-4' : 'translate-x-0',
          )}
        />
      </button>
    );
  },
);

Toggle.displayName = 'Toggle';

export { Toggle };
