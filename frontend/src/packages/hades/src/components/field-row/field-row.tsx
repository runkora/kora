import * as React from 'react';
import { cn } from '../../utils';
import { settingsRowSurfaceVariants } from '../setting-row/settings-row-surface';

/** @tier 1 — Zero kora domain knowledge. A recessed row for a value the user reads or copies. */

export interface FieldRowProps extends React.HTMLAttributes<HTMLDivElement> {
  /** Leading element: an icon, a status marker, or a short key. */
  leading?: React.ReactNode;
  /** Trailing actions, e.g. Copy / Reveal / Rotate buttons. */
  actions?: React.ReactNode;
}

/**
 * FieldRow
 *
 * A recessed row for a value the user reads or copies rather than edits: an
 * endpoint URL, a masked secret, a resolved identifier. Sits on the background
 * step rather than the surface, so it reads as inset into the panel and clearly
 * distinct from an editable input.
 *
 * ```tsx
 * <FieldRow leading={<Icon name="lock" size="2xs" />} actions={<Button size="xs">Copy</Button>}>
 *   <Text mono size="caption">https://…</Text>
 * </FieldRow>
 * ```
 */
const FieldRow = React.forwardRef<HTMLDivElement, FieldRowProps>(
  ({ leading, actions, className, children, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="field-row"
      className={cn(settingsRowSurfaceVariants({ density: 'compact' }), className)}
      {...props}
    >
      {leading != null && <span className="inline-flex shrink-0">{leading}</span>}
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {actions != null && <span className="flex shrink-0 items-center gap-1.5">{actions}</span>}
    </div>
  ),
);
FieldRow.displayName = 'FieldRow';

export { FieldRow };
