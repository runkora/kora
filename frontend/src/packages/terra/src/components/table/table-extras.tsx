import * as React from 'react';

/**
 * Terra – TableContainer
 *
 * Outer card shell. Wrap `TableToolbar` + `TableRoot` + `Table` inside this
 * to get the standard rounded border and overflow clipping.
 *
 * ```tsx
 * <TableContainer>
 *   <TableToolbar>…</TableToolbar>
 *   <TableRoot>
 *     <Table>…</Table>
 *   </TableRoot>
 * </TableContainer>
 * ```
 */
export function TableContainer({ children }: { children: React.ReactNode }) {
  return (
    // `min-w-0` lets the container shrink below its table's intrinsic width when
    // it is a flex item, so the inner `TableRoot` `overflow-x-auto` actually
    // engages instead of the table forcing the whole page wider on mobile.
    <div className="rounded-lg border border-border bg-surface overflow-hidden min-w-0">
      {children}
    </div>
  );
}
