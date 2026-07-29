import * as React from 'react';
import { cva } from 'class-variance-authority';
import { cn } from '../../utils';
import {
  settingsRowDescriptionVariants,
  settingsRowLabelVariants,
  settingsRowSurfaceVariants,
} from '../setting-row/settings-row-surface';
import { toggleKnobVariants, toggleTrackVariants } from '../toggle/toggle-visuals';
import { Text } from '../typography/typography';

/** @tier 1 — Zero Orbit domain knowledge. A labelled on/off setting row. */

/**
 * The row-as-switch container: the shared settings-row surface in its
 * interactive form, so a mixed pane of `SettingRow`, `ToggleRow` and
 * `DangerZone` reads as one stack.
 */
const toggleRowVariants = ({ disabled = false }: { disabled?: boolean } = {}) =>
  settingsRowSurfaceVariants({ interactive: true, disabled });

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
 * A setting the user switches on or off.
 *
 * **Two visual modes, one name.** The props are a discriminated union and the
 * two arms look genuinely different — know which one you are asking for:
 *
 * - **Switch form** (`checked` + `onChange`): a bordered card on the shared
 *   settings-row surface. The whole row is the switch — one `role="switch"`
 *   button, so Tab reaches it, Space and Enter toggle it, and the click target
 *   is the row rather than a 36px sliver. The label and description recede
 *   while the setting is off, so a column of rows shows what is active without
 *   being read. Stacks with `SettingRow` and `DangerZone`.
 * - **Composed form** (`control`): a bare, borderless label pairing beside a
 *   control the caller supplies (a Checkbox, a radio). No surface, no card, no
 *   switch of its own. Use it inside a form or a group of options, not in a
 *   settings stack, where it would read as an unfinished row.
 *
 * ```tsx
 * <ToggleRow
 *   checked={streaming}
 *   onChange={setStreaming}
 *   label="Streaming responses"
 *   description="Emit tokens incrementally rather than buffering."
 * />
 *
 * <ToggleRow
 *   control={<Checkbox id="tos" checked={agreed} onCheckedChange={setAgreed} />}
 *   htmlFor="tos"
 *   label="I agree to the terms"
 * />
 * ```
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
      {/*
        The switch is drawn, not nested: a `Toggle` here would be a button
        inside a button. Same `toggle-visuals` recipe, so it is the same switch.
      */}
      <span
        aria-hidden="true"
        data-slot="toggle-row-track"
        className={cn(toggleTrackVariants({ checked }))}
      >
        <span className={cn(toggleKnobVariants({ checked }))} />
      </span>
      <span data-slot="toggle-row-text" className={cn(toggleTextVariants({ checked }))}>
        <span className={cn('block', settingsRowLabelVariants())}>{label}</span>
        {description != null && (
          <span className={cn('block', settingsRowDescriptionVariants())}>{description}</span>
        )}
      </span>
    </button>
  );
});

ToggleRow.displayName = 'ToggleRow';

export { ToggleRow, toggleRowVariants };
