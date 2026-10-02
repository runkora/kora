import * as React from 'react';
import { cn } from '../../utils';

/**
 * Table — raw primitives
 *
 * Semantic HTML table building blocks for kora's dense, dark, technical
 * aesthetic — hairline row dividers, monospaced values, horizontal overflow
 * scrolling via `TableRoot`.
 *
 * ## When to use these primitives
 *
 * Use raw primitives only for **static, display-only** tables — data that is
 * read-only with no sort, filter, or column-visibility controls.
 *
 * | Use case | Component |
 * |---|---|
 * | Sort / filter / column visibility | `DataTable` |
 * | Static display-only | Raw primitives (`Table`, `TableRow`, …) |
 * | Suspense skeleton mirroring `DataTable` layout | Raw primitives |
 * | Inside `DataTable` itself | Raw primitives (internal) |
 *
 * For any interactive table, use `DataTable` instead — it encapsulates
 * TanStack Table, toolbar, search, filters, sort, and column visibility so
 * consumers only declare columns and pass data.
 *
 * ```tsx
 * // Static display-only — raw primitives are correct here
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
        'focus-visible:outline-2 focus-visible:outline-offset-[-1px] focus-visible:outline-accent',
        className,
      )}
      {...props}
    />
  ),
);
TableRow.displayName = 'TableRow';

type TableHeadAlign = 'left' | 'center' | 'right';

const TABLE_HEAD_ALIGN: Record<TableHeadAlign, string> = {
  left: 'text-left',
  center: 'text-center',
  right: 'text-right',
};

export interface TableHeadProps extends React.ThHTMLAttributes<HTMLTableCellElement> {
  /**
   * Horizontal text alignment for the header cell.
   *
   * Defaults to `'left'`. Use `'right'` for numeric or action columns to
   * mirror the alignment of body cells beneath them.
   *
   * ```tsx
   * <TableHead align="right">Amount</TableHead>
   * ```
   */
  align?: TableHeadAlign;

  /**
   * When `true`, visually hides the header cell while keeping it accessible
   * to screen readers. Emits `sr-only` — no visible text is rendered.
   *
   * Use for action columns where the heading is implied by context.
   *
   * ```tsx
   * <TableHead srOnly>Actions</TableHead>
   * ```
   */
  srOnly?: boolean;
}

/**
 * TableHead
 *
 * Column header cell (`<th>`). Uppercase, spaced tracking, and `text-subtle`
 * colour create clear visual separation from body content.
 *
 * Use `align="right"` for numeric or action columns. Use `srOnly` for
 * implicit action columns that need no visible label.
 */
const TableHead = React.forwardRef<HTMLTableCellElement, TableHeadProps>(
  ({ className, align = 'left', srOnly = false, ...props }, ref) => (
    <th
      ref={ref}
      className={cn(
        'h-10 px-4 align-middle',
        TABLE_HEAD_ALIGN[align],
        'text-xs font-medium uppercase tracking-wider text-subtle',
        'whitespace-nowrap',
        '[&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]',
        srOnly && 'sr-only',
        className,
      )}
      {...props}
    />
  ),
);
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

// ─── Sort utilities ───────────────────────────────────────────────────────────

/**
 * The three possible sort directions for a column.
 * `null` means unsorted.
 */
export type SortDir = 'asc' | 'desc' | null;

/**
 * Advances the sort cycle: null → asc → desc → null.
 * Pass the column's current `SortDir` and apply the returned value.
 */
export function nextSortDir(current: SortDir): SortDir {
  if (current === null) return 'asc';
  if (current === 'asc') return 'desc';
  return null;
}

// ─── Column visibility ────────────────────────────────────────────────────────

/**
 * Describes the show/hide state of a single table column.
 * Used by `TableToolbar` to render the column visibility popover.
 */
export interface TableColumnVisibility {
  /** Unique column identifier — must match the key you use in your state. */
  id: string;
  /** Human-readable label shown in the Columns toggle popover. */
  label: string;
  /** Whether the column is currently visible. */
  visible: boolean;
}

// ─── TableEmpty ───────────────────────────────────────────────────────────────

/**
 * TableEmpty
 *
 * Drop-in empty-state row. Renders a single full-width `<td>` spanning all
 * columns with a centred message. Use inside `TableBody` when your filtered
 * or fetched result set is empty.
 *
 * ```tsx
 * <TableBody>
 *   {rows.length === 0
 *     ? <TableEmpty colSpan={6} />
 *     : rows.map(r => <TableRow key={r.id}>…</TableRow>)
 *   }
 * </TableBody>
 * ```
 */
export function TableEmpty({
  colSpan,
  message = 'No records found.',
}: {
  colSpan: number;
  message?: string;
}) {
  return (
    <tr>
      <td colSpan={colSpan} className="px-4 py-10 text-center text-sm text-subtle">
        {message}
      </td>
    </tr>
  );
}
