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
    <div className="rounded-lg border border-border bg-surface overflow-hidden">{children}</div>
  );
}
