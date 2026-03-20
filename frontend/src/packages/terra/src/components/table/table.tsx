import * as React from 'react';
import { cn } from '../../utils';

/**
 * Table
 *
 * A full-featured data table primitive composed of semantic HTML table parts.
 * Designed for Orbit's dense, dark, technical aesthetic — hairline dividers
 * between rows, monospaced values, and sticky headers on overflow.
 *
 * Compose with `TableHeader`, `TableBody`, `TableHead`, `TableRow`, and
 * `TableCell` for consistent layout and spacing.
 *
 * The outer `TableRoot` wrapper handles horizontal overflow so the table
 * scrolls cleanly inside cards or page sections.
 *
 * ```tsx
 * <TableRoot>
 *   <Table>
 *     <TableHeader>
 *       <TableRow>
 *         <TableHead>Agent</TableHead>
 *         <TableHead>Status</TableHead>
 *       </TableRow>
 *     </TableHeader>
 *     <TableBody>
 *       <TableRow>
 *         <TableCell>apollo-3</TableCell>
 *         <TableCell><Badge variant="healthy">Healthy</Badge></TableCell>
 *       </TableRow>
 *     </TableBody>
 *   </Table>
 * </TableRoot>
 * ```
 */

/**
 * TableRoot
 *
 * Scrollable overflow wrapper. Always use this as the outermost container so
 * wide tables scroll horizontally without affecting the page layout.
 */
const TableRoot = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn('relative w-full overflow-x-auto', className)} {...props} />
  ),
);
TableRoot.displayName = 'TableRoot';

/**
 * Table
 *
 * Core `<table>` element. Sets full width, collapsed borders, and the
 * base monospaced-body typography expected of data-dense control-plane UIs.
 */
const Table = React.forwardRef<HTMLTableElement, React.HTMLAttributes<HTMLTableElement>>(
  ({ className, ...props }, ref) => (
    <table ref={ref} className={cn('w-full caption-bottom text-sm', className)} {...props} />
  ),
);
Table.displayName = 'Table';

/**
 * TableCaption
 *
 * Optional `<caption>` rendered below the table body. Use for short
 * supplementary metadata (row count, last-refreshed time, etc.).
 */
const TableCaption = React.forwardRef<
  HTMLTableCaptionElement,
  React.HTMLAttributes<HTMLTableCaptionElement>
>(({ className, ...props }, ref) => (
  <caption ref={ref} className={cn('mt-3 text-xs text-subtle text-center', className)} {...props} />
));
TableCaption.displayName = 'TableCaption';

/**
 * TableHeader
 *
 * Wraps `<thead>`. Column headings inside sit on `bg-surface-elevated` to
 * distinguish them visually from body rows without requiring a heavy border.
 */
const TableHeader = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <thead ref={ref} className={cn('bg-surface-elevated', className)} {...props} />
));
TableHeader.displayName = 'TableHeader';

/**
 * TableBody
 *
 * Wraps `<tbody>`. The last row has no bottom border so the table sits flush
 * against its containing surface without a double-border artefact.
 */
const TableBody = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <tbody ref={ref} className={cn('[&_tr:last-child]:border-0', className)} {...props} />
));
TableBody.displayName = 'TableBody';

/**
 * TableFooter
 *
 * Wraps `<tfoot>`. Uses `bg-surface-elevated` to mirror the header, suitable
 * for totals rows or pagination controls.
 */
const TableFooter = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <tfoot
    ref={ref}
    className={cn('bg-surface-elevated border-t border-border-subtle font-medium', className)}
    {...props}
  />
));
TableFooter.displayName = 'TableFooter';

/**
 * TableRow
 *
 * A single `<tr>`. Rows have a bottom hairline and a subtle hover tint to aid
 * scanning in dense lists. Apply `data-selected` or `aria-selected` to
 * highlight a selected row.
 */
const TableRow = React.forwardRef<HTMLTableRowElement, React.HTMLAttributes<HTMLTableRowElement>>(
  ({ className, ...props }, ref) => (
    <tr
      ref={ref}
      className={cn(
        'border-b border-border-subtle transition-colors duration-100',
        'hover:bg-surface-elevated/60',
        'data-[selected=true]:bg-accent/10',
        className,
      )}
      {...props}
    />
  ),
);
TableRow.displayName = 'TableRow';

/**
 * TableHead
 *
 * Column header cell (`<th>`). Uppercase, spaced tracking, and `text-subtle`
 * colour create clear visual separation from body content.
 */
const TableHead = React.forwardRef<
  HTMLTableCellElement,
  React.ThHTMLAttributes<HTMLTableCellElement>
>(({ className, ...props }, ref) => (
  <th
    ref={ref}
    className={cn(
      'h-10 px-4 text-left align-middle',
      'text-xs font-medium uppercase tracking-wider text-subtle',
      'whitespace-nowrap',
      '[&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]',
      className,
    )}
    {...props}
  />
));
TableHead.displayName = 'TableHead';

/**
 * TableCell
 *
 * Standard body cell (`<td>`). Comfortable vertical padding for readability
 * in dense lists. Use `font-mono` on numeric or ID columns for alignment.
 */
const TableCell = React.forwardRef<
  HTMLTableCellElement,
  React.TdHTMLAttributes<HTMLTableCellElement>
>(({ className, ...props }, ref) => (
  <td
    ref={ref}
    className={cn(
      'px-4 py-3 align-middle text-sm text-foreground',
      '[&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]',
      className,
    )}
    {...props}
  />
));
TableCell.displayName = 'TableCell';

export {
  TableRoot,
  Table,
  TableCaption,
  TableHeader,
  TableBody,
  TableFooter,
  TableRow,
  TableHead,
  TableCell,
};
