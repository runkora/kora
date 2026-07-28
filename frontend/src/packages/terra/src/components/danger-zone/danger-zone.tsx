import * as React from 'react';
import { cva } from 'class-variance-authority';
import { cn } from '../../utils';

/** @tier 1 — Zero Orbit domain knowledge. The destructive action at the foot of a settings pane. */

/**
 * Error tint at 7% rather than the 5% used by `Callout`/`Alert`: this row sits
 * alone at the foot of a pane with no icon or heading to carry the warning, so
 * the surface has to do it.
 */
const dangerZoneVariants = cva([
  'flex',
  'items-center',
  'justify-between',
  'gap-[12px]',
  'rounded-md',
  'border',
  'border-status-error/30',
  'bg-status-error/[0.07]',
  'px-[14px]',
  'py-[13px]',
]);

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
        <div className="text-[12.5px] font-semibold text-status-error">{title}</div>
        {description != null && (
          <div className="mt-[2px] text-[11.5px] text-muted">{description}</div>
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
