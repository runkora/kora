import * as React from 'react';
import { cva } from 'class-variance-authority';
import { cn } from '../../utils';
import {
  settingsRowDescriptionVariants,
  settingsRowLabelVariants,
  settingsRowSurfaceVariants,
} from './settings-row-surface';

/** @tier 1 — Zero Orbit domain knowledge. A read-only settings row: label, description, value. */

/**
 * The plain settings-row container. An alias for the shared surface
 * (`settings-row-surface.ts`), which `ToggleRow` and `DangerZone` also draw
 * from, so a settings pane made of all three reads as one stack.
 */
const settingRowVariants = () => settingsRowSurfaceVariants();

const settingValueVariants = cva([
  'shrink-0',
  'font-mono',
  'font-medium',
  'text-[12px]',
  'text-foreground',
  'rounded-sm',
  'border',
  'border-border-subtle',
  'bg-surface',
  'px-[10px]',
  'py-[5px]',
]);

export interface SettingRowProps extends Omit<React.HTMLAttributes<HTMLDivElement>, 'children'> {
  /** What the setting is called, e.g. "Default model". */
  label: string;
  /** One line of supporting context under the label. */
  description?: string;
  /** The setting's current value, rendered in a recessed monospaced chip. */
  value: React.ReactNode;
}

/**
 * SettingRow
 *
 * A single configured setting shown as label, optional description, and its
 * current value. Read-only: it reports state rather than editing it, so the
 * value sits in a recessed monospaced chip that reads as data, not as an input.
 *
 * Sibling of `FieldRow` (one copyable value) and `ToggleRow` (a setting the
 * user switches). Use those when the row is a value to copy or a control to
 * operate respectively.
 *
 * ```tsx
 * <SettingRow
 *   label="Default model"
 *   description="Used when a run does not pin one."
 *   value="claude-sonnet-4-5"
 * />
 * ```
 */
const SettingRow = React.forwardRef<HTMLDivElement, SettingRowProps>(
  ({ label, description, value, className, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="setting-row"
      className={cn(settingRowVariants(), className)}
      {...props}
    >
      <div className="min-w-0 flex-1">
        <div className={cn(settingsRowLabelVariants())}>{label}</div>
        {description != null && (
          <div className={cn(settingsRowDescriptionVariants())}>{description}</div>
        )}
      </div>
      <div data-slot="setting-row-value" className={cn(settingValueVariants())}>
        {value}
      </div>
    </div>
  ),
);
SettingRow.displayName = 'SettingRow';

export { SettingRow, settingRowVariants };
export {
  settingsRowSurfaceVariants,
  settingsRowLabelVariants,
  settingsRowDescriptionVariants,
} from './settings-row-surface';
