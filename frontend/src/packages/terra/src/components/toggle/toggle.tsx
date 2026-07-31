import * as React from 'react';
import { cn } from '../../utils';
import { toggleKnobVariants, toggleTrackVariants } from './toggle-visuals';

export interface ToggleProps {
  /** The controlled checked state. */
  checked: boolean;
  /** Called with the new state when the user clicks the toggle. */
  onChange: (checked: boolean) => void;
  /** Associates the toggle with an external label element. */
  id?: string;
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
  ({ checked, onChange, id, disabled = false, className, ...ariaProps }, ref) => {
    return (
      <button
        ref={ref}
        id={id}
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => !disabled && onChange(!checked)}
        className={cn(
          toggleTrackVariants({ checked, disabled }),
          'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
          !disabled && 'cursor-pointer',
          className,
        )}
        {...ariaProps}
      >
        <span data-slot="toggle-knob" className={cn(toggleKnobVariants({ checked }))} />
      </button>
    );
  },
);

Toggle.displayName = 'Toggle';

export { Toggle };
