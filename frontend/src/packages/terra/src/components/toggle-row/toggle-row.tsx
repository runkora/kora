import * as React from 'react';
import { cva } from 'class-variance-authority';
import { cn } from '../../utils';
import { Text } from '../typography/typography';

/** @tier 1 — Zero Orbit domain knowledge. A labelled on/off setting row. */

/** Shared with `SettingRow` so a mixed settings pane reads as one stack. */
const toggleRowVariants = cva(
  [
    'flex',
    'w-full',
    'items-center',
    'gap-[12px]',
    'rounded-md',
    'border',
    'border-border-subtle',
    'bg-background',
    'px-[14px]',
    'py-[12px]',
    'text-left',
    'transition-colors',
    'focus-visible:outline-2',
    'focus-visible:outline-offset-2',
    'focus-visible:outline-accent',
  ],
  {
    variants: {
      disabled: {
        true: 'cursor-not-allowed opacity-40',
        false: 'cursor-pointer',
      },
    },
    defaultVariants: { disabled: false },
  },
);

/** The track carries the on/off signal; the knob only carries position. */
const toggleTrackVariants = cva(
  ['relative', 'h-[20px]', 'w-[34px]', 'shrink-0', 'rounded-full', 'transition-colors'],
  {
    variants: {
      checked: {
        true: 'bg-accent',
        false: 'bg-surface-elevated',
      },
    },
    defaultVariants: { checked: false },
  },
);

const toggleKnobVariants = cva(
  [
    'pointer-events-none',
    'absolute',
    'top-[2px]',
    'h-[16px]',
    'w-[16px]',
    'rounded-full',
    'bg-foreground',
    'shadow',
    'transition-[left]',
    'duration-150',
  ],
  {
    variants: {
      checked: {
        true: 'left-[16px]',
        false: 'left-[2px]',
      },
    },
    defaultVariants: { checked: false },
  },
);

/** Off is not "disabled", but it is inactive, so the copy recedes with it. */
const toggleTextVariants = cva(['min-w-0', 'flex-1', 'transition-opacity'], {
  variants: {
    checked: {
      true: 'opacity-100',
      false: 'opacity-60',
    },
  },
  defaultVariants: { checked: false },
});

interface ToggleRowBaseProps {
  /** What the setting is called, e.g. "Streaming responses". */
  label: string;
  /** One line of supporting context under the label. */
  description?: string;
  /** Prevents interaction and dims the row. */
  disabled?: boolean;
  className?: string;
}

/** Self-contained form: the row owns the switch. */
export interface ToggleRowSwitchProps extends ToggleRowBaseProps {
  /** Controlled on/off state. */
  checked: boolean;
  /** Called with the next state when the row is activated. */
  onChange: (next: boolean) => void;
  control?: never;
  htmlFor?: never;
}

/** Composed form: the caller supplies the control (Checkbox, radio, custom). */
export interface ToggleRowControlProps extends ToggleRowBaseProps {
  /** The control element rendered at the leading edge. */
  control: React.ReactNode;
  /** Associates the label with the control via `htmlFor`. */
  htmlFor?: string;
  checked?: never;
  onChange?: never;
}

export type ToggleRowProps = ToggleRowSwitchProps | ToggleRowControlProps;

/**
 * ToggleRow
 *
 * A setting the user switches on or off. In its default form the whole row is
 * the switch: it renders as a single `role="switch"` button, so Tab reaches it,
 * Space and Enter toggle it, and the entire row is the click target rather than
 * a 34px sliver. The label and description recede while the setting is off, so
 * a column of rows shows what is active without being read.
 *
 * The container matches `SettingRow`, so mixed settings panes stack cleanly.
 *
 * ```tsx
 * <ToggleRow
 *   checked={streaming}
 *   onChange={setStreaming}
 *   label="Streaming responses"
 *   description="Emit tokens incrementally rather than buffering."
 * />
 * ```
 *
 * Pass `control` instead of `checked`/`onChange` for the composed form, where
 * the caller supplies a Checkbox or radio and the row is a bare label pairing
 * rather than a switch of its own.
 */
const ToggleRow = React.forwardRef<HTMLElement, ToggleRowProps>((props, ref) => {
  if (props.onChange === undefined) {
    const { label, description, control, htmlFor, disabled = false, className } = props;
    return (
      <div
        ref={ref as React.Ref<HTMLDivElement>}
        data-slot="toggle-row"
        className={cn(
          'flex flex-row items-start gap-3',
          disabled && 'opacity-50 pointer-events-none',
          className,
        )}
      >
        <div className="mt-0.5 shrink-0">{control}</div>
        <div className="flex flex-col gap-0.5 min-w-0">
          {htmlFor ? (
            <label
              htmlFor={htmlFor}
              className="text-sm font-medium leading-none text-foreground select-none cursor-pointer"
            >
              {label}
            </label>
          ) : (
            <Text size="body" weight="emphasis" className="leading-none">
              {label}
            </Text>
          )}
          {description !== undefined && (
            <Text size="supporting" color="muted" className="leading-snug">
              {description}
            </Text>
          )}
        </div>
      </div>
    );
  }

  const { label, description, checked, onChange, disabled = false, className } = props;

  return (
    <button
      ref={ref as React.Ref<HTMLButtonElement>}
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      data-slot="toggle-row"
      data-state={checked ? 'checked' : 'unchecked'}
      onClick={() => {
        if (!disabled) onChange(!checked);
      }}
      className={cn(toggleRowVariants({ disabled }), className)}
    >
      <span data-slot="toggle-row-track" className={cn(toggleTrackVariants({ checked }))}>
        <span className={cn(toggleKnobVariants({ checked }))} />
      </span>
      <span data-slot="toggle-row-text" className={cn(toggleTextVariants({ checked }))}>
        <span className="block text-[12.5px] font-semibold text-foreground">{label}</span>
        {description != null && (
          <span className="mt-[2px] block text-[11.5px] text-muted">{description}</span>
        )}
      </span>
    </button>
  );
});

ToggleRow.displayName = 'ToggleRow';

export { ToggleRow, toggleRowVariants };
