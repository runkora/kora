import * as React from 'react';
import { cn } from '../../utils';
import { sectionLabelVariants } from '../section-label';

/** @tier 1 — Zero Orbit domain knowledge. A fixed-column list surface: header row plus body rows. */

/**
 * DataGrid
 *
 * A fixed-column list: a header row and body rows sharing one grid template, in
 * a bordered surface. Use when the columns must line up down the page at widths
 * you choose, and the rows carry composed content — an icon tile, a chip, a
 * sparkline — rather than plain text.
 *
 * Prefer `DataTable` when you need sorting, search, column visibility and
 * pagination out of the box. This is the lower-level primitive underneath that
 * kind of surface: it lays out and does nothing else.
 *
 * Column widths and the column gap are passed as CSS lengths rather than
 * spacing tokens. They are structural — how wide is the "last fired" column —
 * not theme spacing, and snapping them to a scale would stop columns aligning
 * with the content they were sized for.
 *
 * ```tsx
 * <DataGrid columns="1fr 168px 148px 74px">
 *   <DataGridHeader cells={['Trigger', 'Fires', 'Agent', 'Enabled']} />
 *   {rows.map(r => (
 *     <DataGridRow key={r.id} onClick={() => open(r)}>…</DataGridRow>
 *   ))}
 * </DataGrid>
 * ```
 */
export interface DataGridProps extends React.HTMLAttributes<HTMLDivElement> {
  /** `grid-template-columns` value shared by the header and every row. */
  columns: string;
  /** Gap between columns. Any CSS length. @default '14px' */
  columnGap?: string;
}

const DataGridContext = React.createContext<{ columns: string; columnGap: string }>({
  columns: '1fr',
  columnGap: '14px',
});

const DataGrid = React.forwardRef<HTMLDivElement, DataGridProps>(
  ({ columns, columnGap = '14px', className, children, ...props }, ref) => (
    <DataGridContext.Provider value={{ columns, columnGap }}>
      <div
        ref={ref}
        data-slot="data-grid"
        role="table"
        className={cn(
          'overflow-hidden rounded-lg border border-border-subtle bg-surface',
          // The final row closes against the frame rather than drawing a rule
          // into it. Derived here — "the row with no row after it" — so no
          // caller has to know which row is last.
          '[&>[data-slot=data-grid-row]:not(:has(~[data-slot=data-grid-row]))]:border-b-0',
          className,
        )}
        {...props}
      >
        {children}
      </div>
    </DataGridContext.Provider>
  ),
);
DataGrid.displayName = 'DataGrid';

/** One column heading. A string is left-aligned; use the object form to align right. */
export type DataGridHeaderCell = string | { label: string; align?: 'start' | 'end' };

export interface DataGridHeaderProps extends Omit<
  React.HTMLAttributes<HTMLDivElement>,
  'children'
> {
  /** Column headings, one per column in the grid template. */
  cells: DataGridHeaderCell[];
}

/**
 * Column headings. Tracked small caps, so they label without competing with the
 * rows beneath.
 *
 * Takes labels rather than children so callers never hand-roll the cell markup,
 * which is what keeps every grid's header identical.
 */
const DataGridHeader = React.forwardRef<HTMLDivElement, DataGridHeaderProps>(
  ({ cells, className, ...props }, ref) => {
    const { columns, columnGap } = React.useContext(DataGridContext);
    return (
      <div
        ref={ref}
        data-slot="data-grid-header"
        role="row"
        className={cn(
          'grid items-center border-b border-border-subtle px-4 py-[9px]',
          // The same overline recipe as SectionLabel, so a grid header and a
          // panel label read as one treatment rather than two near-misses.
          sectionLabelVariants(),
          className,
        )}
        style={{ gridTemplateColumns: columns, columnGap }}
        {...props}
      >
        {cells.map((c, i) => {
          const label = typeof c === 'string' ? c : c.label;
          const align = typeof c === 'string' ? 'start' : (c.align ?? 'start');
          return (
            <span
              // Columns are fixed positions, so the index is the identity.
              key={i}
              role="columnheader"
              className={align === 'end' ? 'text-right' : undefined}
            >
              {label}
            </span>
          );
        })}
      </div>
    );
  },
);
DataGridHeader.displayName = 'DataGridHeader';

export interface DataGridRowProps extends React.HTMLAttributes<HTMLDivElement> {
  /** When set, the row becomes clickable and gains a hover surface. */
  onClick?: React.MouseEventHandler<HTMLDivElement>;
  /** Accessible label for a clickable row, e.g. "Open pr-review". */
  rowLabel?: string;
}

const DataGridRow = React.forwardRef<HTMLDivElement, DataGridRowProps>(
  ({ onClick, rowLabel, className, children, ...props }, ref) => {
    const { columns, columnGap } = React.useContext(DataGridContext);
    const clickable = onClick !== undefined;
    return (
      <div
        ref={ref}
        data-slot="data-grid-row"
        role="row"
        // Rows are reachable and activatable from the keyboard when clickable,
        // so the list is not a mouse-only surface.
        tabIndex={clickable ? 0 : undefined}
        aria-label={rowLabel}
        onClick={onClick}
        onKeyDown={
          clickable
            ? (e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onClick(e as unknown as React.MouseEvent<HTMLDivElement>);
                }
              }
            : undefined
        }
        className={cn(
          'grid items-center border-b border-border-subtle px-4 py-3',
          clickable &&
            'cursor-pointer transition-colors hover:bg-surface-elevated focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent',
          className,
        )}
        style={{ gridTemplateColumns: columns, columnGap }}
        {...props}
      >
        {/* Each child is one column, so each child is one cell. Wrapped here
            rather than asking every consumer to wrap its own: a role="row"
            whose children carry no cell role describes a table with rows and
            no cells, which is worse for a screen reader than no table role at
            all. The wrapper takes over as the grid item, so it carries the
            min-w-0 that lets a truncating child actually truncate. */}
        {React.Children.map(children, (child) => (
          <div role="cell" className="min-w-0">
            {child}
          </div>
        ))}
      </div>
    );
  },
);
DataGridRow.displayName = 'DataGridRow';

/**
 * Toolbar strip above the header: filters, search, column controls.
 *
 * Inside the grid's frame rather than floating above it, so the controls read
 * as belonging to this list rather than to the page. That also means a filter
 * that matches nothing cannot take its own escape hatch off screen.
 */
const DataGridToolbar = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, children, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="data-grid-toolbar"
      className={cn(
        'flex items-center gap-3 border-b border-border-subtle px-4 py-[11px]',
        className,
      )}
      {...props}
    >
      {children}
    </div>
  ),
);
DataGridToolbar.displayName = 'DataGridToolbar';

/** Footer strip for counts and pagination, inside the same frame. */
const DataGridFooter = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, children, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="data-grid-footer"
      className={cn(
        'flex items-center gap-3 border-t border-border-subtle px-4 py-[11px]',
        className,
      )}
      {...props}
    >
      {children}
    </div>
  ),
);
DataGridFooter.displayName = 'DataGridFooter';

/** Centred message shown inside the frame when there are no rows. */
const DataGridEmpty = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, children, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="data-grid-empty"
      className={cn('flex flex-col items-center gap-2 px-5 py-10 text-center', className)}
      {...props}
    >
      {children}
    </div>
  ),
);
DataGridEmpty.displayName = 'DataGridEmpty';

export { DataGrid, DataGridToolbar, DataGridHeader, DataGridRow, DataGridFooter, DataGridEmpty };
