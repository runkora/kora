import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DataTable, type DataTableColumn } from './data-table';
import { TooltipProvider } from '../tooltip';

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

/**
 * A sortable column with a headerTooltip renders two controls in one header.
 * They have to be siblings: a button inside a button is invalid HTML, and a
 * click on the inner one bubbles into a sort the user did not ask for.
 */
describe('headerTooltip on a sortable column', () => {
  type Row = { id: string; price: number };

  const rows: Row[] = [
    { id: 'a', price: 1 },
    { id: 'b', price: 2 },
  ];

  const columns: DataTableColumn<Row>[] = [
    { id: 'id', header: 'Model', cell: (r) => r.id },
    {
      id: 'price',
      header: 'Effective Price',
      headerTooltip: 'The per-1M token price for a standard text request.',
      sortable: true,
      sortAccessor: (r) => r.price,
      cell: (r) => String(r.price),
    },
  ];

  it('keeps the info control outside the sort control', () => {
    render(
      <TooltipProvider>
        <DataTable data={rows} columns={columns} getRowKey={(r) => r.id} />
      </TooltipProvider>,
    );

    const sort = screen.getByRole('button', { name: 'Sort by Effective Price' });
    const info = screen.getByRole('button', { name: 'More information about Effective Price' });

    expect(sort.contains(info)).toBe(false);
  });

  it('does not sort when the info control is clicked', async () => {
    const onSortChange = vi.fn();
    render(
      <TooltipProvider>
        <DataTable
          data={rows}
          columns={columns}
          getRowKey={(r) => r.id}
          onSortChange={onSortChange}
        />
      </TooltipProvider>,
    );

    await userEvent.click(
      screen.getByRole('button', { name: 'More information about Effective Price' }),
    );
    expect(onSortChange).not.toHaveBeenCalled();
  });
});
