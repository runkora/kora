import { cva } from 'class-variance-authority';

/**
 * The switch visuals: one track, one knob.
 *
 * Extracted here because `Toggle` is not the only thing that draws a switch —
 * `ToggleRow` renders the same control inside a row that is itself the button,
 * so it cannot nest an interactive `Toggle`. Both draw from this one recipe, so
 * the switch looks the same wherever it appears.
 *
 * These are presentation only. Whatever renders them owns the `role="switch"`,
 * `aria-checked` and the click handling.
 */

/**
 * The track carries the on/off signal. Off is a translucent wash of `foreground`
 * rather than a surface token: the light surfaces are all near-white, so a
 * surface fill disappears on some of them, while a wash stays distinct on any.
 */
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
        false: 'bg-foreground/45',
      },
      disabled: {
        true: 'cursor-not-allowed opacity-40',
        false: '',
      },
    },
    defaultVariants: { checked: false, disabled: false },
  },
);

/** The knob carries position; on it takes `accent-foreground` so it contrasts the accent track in both themes. */
const toggleKnobVariants = cva(
  [
    'pointer-events-none',
    'inline-block',
    'h-4',
    'w-4',
    'rounded-full',
    'shadow',
    'transition-transform',
  ],
  {
    variants: {
      checked: {
        true: 'translate-x-4 bg-accent-foreground',
        false: 'translate-x-0 bg-foreground',
      },
    },
    defaultVariants: { checked: false },
  },
);

export { toggleTrackVariants, toggleKnobVariants };
