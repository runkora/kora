import * as React from 'react';
import { cn } from '../../utils';

export interface DragCardProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * When true, the card is visually dimmed and scaled down to indicate it is
   * being dragged.
   */
  dragging?: boolean;

  /**
   * When true, the card shows an accent border and subtle tint to indicate it
   * is a valid drop target while another card is being dragged.
   */
  dropTarget?: boolean;
}

/**
 * DragCard
 *
 * A draggable card that accepts drag event handlers and expresses visual
 * drag-state via typed props rather than dynamic `className` strings.
 *
 * All standard HTML drag events (`draggable`, `onDragStart`, `onDragOver`,
 * `onDrop`, `onDragLeave`, `onDragEnd`) pass through via `...props`.
 *
 * ```tsx
 * <DragCard
 *   draggable
 *   dragging={draggingIdx === idx}
 *   dropTarget={overIdx === idx && draggingIdx !== null}
 *   onDragStart={() => handleDragStart(idx)}
 *   onDragOver={(e) => handleDragOver(e, idx)}
 *   onDragLeave={handleDragLeave}
 *   onDrop={() => handleDrop(idx)}
 *   onDragEnd={handleDragEnd}
 * >
 *   …content…
 * </DragCard>
 * ```
 */
export const DragCard = React.forwardRef<HTMLDivElement, DragCardProps>(
  ({ dragging = false, dropTarget = false, className, children, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        'flex gap-3 rounded-md border p-3 transition-all duration-150',
        dragging
          ? 'scale-[0.98] border-border bg-surface-elevated opacity-40'
          : dropTarget
            ? 'border-accent bg-accent/5 shadow-sm'
            : 'border-border bg-surface-elevated',
        className,
      )}
      {...props}
    >
      {children}
    </div>
  ),
);

DragCard.displayName = 'DragCard';
