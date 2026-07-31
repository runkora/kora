import { cva } from 'class-variance-authority';

/**
 * The settings-row surface.
 *
 * One recipe shared by `SettingRow`, `ToggleRow` and `DangerZone` so a settings
 * pane made of all three reads as a single stack rather than three competing row
 * treatments. One recipe, because near-identical copies drift by a pixel, which
 * is exactly the failure this prevents.
 *
 * `tone` covers the only real divergence: the danger row is the same geometry in
 * error tint. `interactive` adds what a row needs when the row itself is the
 * control (full width, left-aligned text, a focus ring).
 *
 * Type sizes here are on the defined scale (12px label, 11px description).
 */
const settingsRowSurfaceVariants = cva(['flex', 'items-center', 'rounded-md', 'border'], {
  variants: {
    /**
     * `compact` is FieldRow's row: a value the user reads or copies, not a
     * setting with a label and description. Its tighter metrics are
     * intentional — a single-line value row at settings-row padding reads as
     * empty — so they live here as a named density rather than as a drifted
     * copy.
     */
    density: {
      default: 'gap-[12px] px-[14px] py-[12px]',
      compact: 'gap-[9px] px-[13px] py-[11px]',
    },
    tone: {
      default: 'border-border-subtle bg-background',
      /**
       * Error tint at 7% rather than the 5% used by `Callout`/`Alert`: a
       * danger row sits alone at the foot of a pane with no icon or heading
       * to carry the warning, so the surface has to do it.
       */
      danger: 'border-status-error/30 bg-status-error/[0.07]',
    },
    /** The whole row is the control: it is a button, so it needs button chrome. */
    interactive: {
      true: [
        'w-full',
        'text-left',
        'transition-colors',
        'focus-visible:outline-2',
        'focus-visible:outline-offset-2',
        'focus-visible:outline-accent',
      ],
      false: '',
    },
    disabled: {
      true: 'cursor-not-allowed opacity-40',
      false: '',
    },
  },
  compoundVariants: [{ interactive: true, disabled: false, class: 'cursor-pointer' }],
  defaultVariants: { density: 'default', tone: 'default', interactive: false, disabled: false },
});

/** The row's name. 12px semibold, tinted by tone. */
const settingsRowLabelVariants = cva(['text-xs', 'font-semibold'], {
  variants: {
    tone: {
      default: 'text-foreground',
      danger: 'text-status-error',
    },
  },
  defaultVariants: { tone: 'default' },
});

/** The supporting line under the label. 11px, muted, in every tone. */
const settingsRowDescriptionVariants = cva(['mt-[2px]', 'text-[11px]', 'text-muted']);

export { settingsRowSurfaceVariants, settingsRowLabelVariants, settingsRowDescriptionVariants };
