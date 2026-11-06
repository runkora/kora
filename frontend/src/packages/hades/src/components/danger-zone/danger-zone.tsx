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
const dangerZoneVariants = (tone: 'danger' | 'warning' = 'danger') =>
  cn(settingsRowSurfaceVariants({ tone }), 'justify-between');

export interface DangerZoneProps extends Omit<React.HTMLAttributes<HTMLDivElement>, 'title'> {
  /** Names the destructive action, e.g. "Delete this agent". May carry an inline accessory such as an info tooltip. */
  title: React.ReactNode;
  /** What is lost and whether it can be undone. */
  description?: string;
  /** The action itself, normally a `<Button variant="destructive">`. */
  action?: React.ReactNode;
  /**
   * `danger` (default) is for an irreversible action. `warning` is the same row
   * in amber, for an action with consequences that can still be recovered from;
   * pair it with `<Button variant="warning">`.
   */
  tone?: 'danger' | 'warning';
  /** Content under the description, inside the surface: a field or a revealed value the action relates to. */
  children?: React.ReactNode;
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
  ({ title, description, action, tone = 'danger', className, children, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="danger-zone"
      className={cn(dangerZoneVariants(tone), className)}
      {...props}
    >
      <div className="min-w-0 flex-1">
        <div className={cn(settingsRowLabelVariants({ tone }))}>{title}</div>
        {description != null && (
          <div className={cn(settingsRowDescriptionVariants())}>{description}</div>
        )}
        {children != null && <div className="mt-2 flex flex-col gap-2">{children}</div>}
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
