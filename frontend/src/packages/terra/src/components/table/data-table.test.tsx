import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { DataTable, type DataTableColumn } from './data-table';

/**
 * Sticky pinned-column behaviour. The first locked column is pinned to the left
 * (`sticky left-0`) with an opaque background and elevated z-index so the
 * primary identifier stays visible while the table scrolls horizontally. This
 * covers both header render paths and the body cell:
 *   - static header   → <TableHead>          (locked, non-sortable)
 *   - sortable header → <TableColumnHeader>  (locked, sortable)
 *   - body cell       → <TableCell>
 * Non-pinned columns — including *additional* locked columns — must not receive
 * the sticky classes (pinning several at left-0 would stack them on top of one
 * another; `locked` is also used merely to keep a column out of the toggle).
 */

interface Row {
  name: string;
  rank: number;
  status: string;
}

const data: Row[] = [{ name: 'apollo', rank: 1, status: 'healthy' }];

function renderTable(columns: DataTableColumn<Row>[]) {
  return render(<DataTable data={data} columns={columns} getRowKey={(r) => r.name} />);
}

describe('DataTable pinned column (sticky)', () => {
  it('pins the first locked column when it is a static header', () => {
    renderTable([
      { id: 'name', header: 'Name', cell: (r) => r.name, locked: true },
      { id: 'status', header: 'Status', cell: (r) => r.status },
    ]);
    expect(screen.getByRole('columnheader', { name: 'Name' })).toHaveClass(
      'sticky',
      'left-0',
      'z-10',
    );
    expect(screen.getByRole('cell', { name: 'apollo' })).toHaveClass('sticky', 'left-0', 'z-10');
  });

  it('pins the first locked column when it is a sortable header', () => {
    renderTable([
      {
        id: 'rank',
        header: 'Rank',
        cell: (r) => String(r.rank),
        locked: true,
        sortable: true,
        sortAccessor: (r) => r.rank,
      },
      { id: 'status', header: 'Status', cell: (r) => r.status },
    ]);
    expect(screen.getByRole('columnheader', { name: /Rank/ })).toHaveClass('sticky', 'left-0');
  });

  it('does not pin non-locked columns', () => {
    renderTable([
      { id: 'name', header: 'Name', cell: (r) => r.name, locked: true },
      { id: 'status', header: 'Status', cell: (r) => r.status },
    ]);
    expect(screen.getByRole('columnheader', { name: 'Status' })).not.toHaveClass('sticky');
    expect(screen.getByRole('cell', { name: 'healthy' })).not.toHaveClass('sticky');
  });

  it('pins only the first locked column when several columns are locked', () => {
    renderTable([
      { id: 'name', header: 'Name', cell: (r) => r.name, locked: true },
      { id: 'rank', header: 'Rank', cell: (r) => String(r.rank), locked: true },
      { id: 'status', header: 'Status', cell: (r) => r.status, locked: true },
    ]);
    // First locked column is pinned…
    expect(screen.getByRole('columnheader', { name: 'Name' })).toHaveClass('sticky');
    expect(screen.getByRole('cell', { name: 'apollo' })).toHaveClass('sticky');
    // …but subsequent locked columns are not (avoids stacking at left-0).
    expect(screen.getByRole('columnheader', { name: 'Rank' })).not.toHaveClass('sticky');
    expect(screen.getByRole('cell', { name: '1' })).not.toHaveClass('sticky');
    expect(screen.getByRole('columnheader', { name: 'Status' })).not.toHaveClass('sticky');
  });

  it('pins nothing when no column is locked', () => {
    renderTable([
      { id: 'name', header: 'Name', cell: (r) => r.name },
      { id: 'status', header: 'Status', cell: (r) => r.status },
    ]);
    expect(screen.getByRole('columnheader', { name: 'Name' })).not.toHaveClass('sticky');
    expect(screen.getByRole('cell', { name: 'apollo' })).not.toHaveClass('sticky');
  });
});
