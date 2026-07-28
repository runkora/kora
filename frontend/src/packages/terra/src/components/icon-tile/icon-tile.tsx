import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

const iconTileVariants = cva(
  ['inline-flex', 'shrink-0', 'items-center', 'justify-center', 'border'],
  {
    variants: {
      /**
       * `accent` marks the row as highlighted; `muted` is the resting state.
       * Keeping both on one surface family means a highlighted row reads as
       * emphasised rather than as a different kind of thing.
       */
      tone: {
        muted: 'bg-surface-overlay border-border-subtle text-muted',
        accent: 'bg-accent-muted border-accent-border text-accent',
      },
      size: {
        sm: 'size-5 rounded-[5px]',
        md: 'size-8 rounded-md',
        /** 36px — the identity tile in a panel header, where the tile is the
         *  subject's marker rather than a row's leading glyph. */
        lg: 'size-9 rounded-md',
      },
    },
    defaultVariants: { tone: 'muted', size: 'md' },
  },
);

export interface IconTileProps
  extends React.HTMLAttributes<HTMLSpanElement>, VariantProps<typeof iconTileVariants> {
  children: React.ReactNode;
}

/**
 * IconTile
 *
 * A small rounded container that gives an icon or initial a fixed footprint, so
 * a column of them lines up regardless of the glyph inside. Used as the leading
 * marker on list rows and as a compact identity chip.
 *
 * ```tsx
 * <IconTile><Icon name="clock" size="xs" /></IconTile>
 * <IconTile size="sm" tone="muted">A</IconTile>
 * ```
 */
const IconTile = React.forwardRef<HTMLSpanElement, IconTileProps>(
  ({ tone, size, className, children, ...props }, ref) => (
    <span
      ref={ref}
      data-slot="icon-tile"
      className={cn(iconTileVariants({ tone, size }), className)}
      {...props}
    >
      {children}
    </span>
  ),
);
IconTile.displayName = 'IconTile';

export { IconTile, iconTileVariants };
