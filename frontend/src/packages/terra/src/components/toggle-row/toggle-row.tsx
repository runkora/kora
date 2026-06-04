import * as React from 'react';
import { cn } from '../../utils';
import { Text } from '../typography/typography';

/** @tier 1 — Zero Orbit domain knowledge. A labelled control row for toggles, checkboxes, and radios. */

export interface ToggleRowProps {
  /** Primary label text. Rendered as an accessible `<label>` when `htmlFor` is supplied. */
  label: string;
  /** Supporting description rendered below the label in muted text. */
  description?: string;
  /**
   * The control element (Toggle, Checkbox, radio input, etc.) rendered at the
   * leading edge. The row uses `items-start` + a small top offset on the control
   * so it optically aligns with the label's cap-height rather than centering
   * on the whole label+description block.
   */
  control: React.ReactNode;
  /** Associates the label with the control via `htmlFor`. */
  htmlFor?: string;
  /** Dims the row and prevents pointer events. */
  disabled?: boolean;
  className?: string;
}

/**
 * ToggleRow
 *
 * A labelled control row. Pairs a toggle, checkbox, or radio with a label and
 * optional description. The control is vertically anchored to the label's
 * cap-height — not centred on the whole block — so it reads correctly whether
 * or not a description is present.
 *
 * ```tsx
 * <ToggleRow
 *   htmlFor="streaming"
 *   label="Enable streaming"
 *   description="Stream responses token by token as they are generated."
 *   control={<Toggle id="streaming" checked={enabled} onChange={setEnabled} />}
 * />
 * ```
 */
const ToggleRow = React.forwardRef<HTMLDivElement, ToggleRowProps>(
  ({ label, description, control, htmlFor, disabled = false, className }, ref) => (
    <div
      ref={ref}
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
  ),
);

ToggleRow.displayName = 'ToggleRow';

export { ToggleRow };
