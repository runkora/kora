import { cva } from 'class-variance-authority';

/**
 * The switch visuals: one track, one knob.
 *
 * Extracted here because `Toggle` is not the only thing that draws a switch —
 * `ToggleRow` renders the same control inside a row that is itself the button,
 * so it cannot nest an interactive `Toggle` and previously re-implemented the
 * track and knob 2px away from these. Both now draw from this recipe, so the
 * switch looks the same wherever it appears.
 *
 * These are presentation only. Whatever renders them owns the `role="switch"`,
 * `aria-checked` and the click handling.
 */

/** The track carries the on/off signal. */
const toggleTrackVariants = cva(
  [
    'relative',
    'inline-flex',
    'h-5',
    'w-9',
    'shrink-0',
    'rounded-full',
    'border-2',
    'border-transparent',
    'transition-colors',
  ],
  {
    variants: {
      checked: {
        true: 'bg-accent',
        false: 'bg-surface-elevated',
      },
      disabled: {
        true: 'cursor-not-allowed opacity-40',
        false: '',
      },
    },
    defaultVariants: { checked: false, disabled: false },
  },
);

/** The knob only carries position. */
const toggleKnobVariants = cva(
  [
    'pointer-events-none',
    'inline-block',
    'h-4',
    'w-4',
    'rounded-full',
    'bg-foreground',
    'shadow',
    'transition-transform',
  ],
  {
    variants: {
      checked: {
        true: 'translate-x-4',
        false: 'translate-x-0',
      },
    },
    defaultVariants: { checked: false },
  },
);

export { toggleTrackVariants, toggleKnobVariants };
