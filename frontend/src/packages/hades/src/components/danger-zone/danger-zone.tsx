import * as React from 'react';
import { cn } from '../../utils';
import {
  settingsRowDescriptionVariants,
  settingsRowLabelVariants,
  settingsRowSurfaceVariants,
} from '../setting-row/settings-row-surface';

/** @tier 1 — Zero kora domain knowledge. The destructive action at the foot of a settings pane. */

/**
 * The shared settings-row surface in its danger tone, so the destructive row
 * lines up with the settings above it and differs only in colour — which is the
 * whole signal. The tint rationale lives with the recipe.
 */
const dangerZoneVariants = () =>
  cn(settingsRowSurfaceVariants({ tone: 'danger' }), 'justify-between');

export interface DangerZoneProps extends Omit<React.HTMLAttributes<HTMLDivElement>, 'title'> {
  /** Names the destructive action, e.g. "Delete this agent". */
  title: string;
  /** What is lost and whether it can be undone. */
  description?: string;
  /** The action itself, normally a `<Button variant="destructive">`. */
  action?: React.ReactNode;
}

/**
 * DangerZone
 *
 * The irreversible action at the foot of a settings pane, fenced off in error
 * tint so it is never mistaken for the settings above it. One action per zone:
 * if a pane has two destructive actions they get two zones, so the description
 * always names exactly what the adjacent button destroys.
 *
 * ```tsx
 * <DangerZone
 *   title="Delete this agent"
 *   description="Removes the agent and all run history. This cannot be undone."
 *   action={<Button variant="destructive" size="xs" onClick={onDelete}>Delete</Button>}
 * />
 * ```
 */
const DangerZone = React.forwardRef<HTMLDivElement, DangerZoneProps>(
  ({ title, description, action, className, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="danger-zone"
      className={cn(dangerZoneVariants(), className)}
      {...props}
    >
      <div className="min-w-0 flex-1">
        <div className={cn(settingsRowLabelVariants({ tone: 'danger' }))}>{title}</div>
        {description != null && (
          <div className={cn(settingsRowDescriptionVariants())}>{description}</div>
        )}
      </div>
      {action != null && (
        <div data-slot="danger-zone-action" className="shrink-0">
          {action}
        </div>
      )}
    </div>
  ),
);
DangerZone.displayName = 'DangerZone';

export { DangerZone, dangerZoneVariants };
