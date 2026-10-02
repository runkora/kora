import { afterEach, describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DataTable, tableMinWidth, type DataTableColumn } from './data-table';
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

describe('DataTable overflow handling', () => {
  const tracks = () => screen.getAllByRole('row')[0].style.gridTemplateColumns;

  it('floors flexible columns so they cannot collapse', () => {
    renderTable([
      { id: 'name', header: 'Name', cell: (r) => r.name },
      { id: 'status', header: 'Status', cell: (r) => r.status, gridWidth: 'minmax(0, 2fr)' },
      { id: 'rank', header: 'Rank', cell: (r) => r.rank, gridWidth: '3rem' },
      { id: 'x', header: 'X', cell: (r) => r.name, gridWidth: '1fr', minWidth: '12rem' },
    ]);
    expect(tracks()).toBe('minmax(5rem, 1fr) minmax(5rem, 2fr) 3rem minmax(12rem, 1fr)');
  });

  it('reads minmax tracks written with irregular spacing', () => {
    renderTable([
      { id: 'name', header: 'Name', cell: (r) => r.name, gridWidth: 'minmax(  0 ,  2fr  )' },
      { id: 'status', header: 'Status', cell: (r) => r.status, gridWidth: 'minmax(0,calc(1fr))' },
      { id: 'rank', header: 'Rank', cell: (r) => r.rank, gridWidth: 'minmax(8rem,1fr)' },
    ]);
    expect(tracks()).toBe('minmax(5rem, 2fr) minmax(5rem, calc(1fr)) minmax(8rem, 1fr)');
  });

  it('parses a long run of spaces without stalling', () => {
    const spaces = ' '.repeat(50_000);
    const started = performance.now();
    renderTable([
      { id: 'name', header: 'Name', cell: (r) => r.name, gridWidth: `minmax(${spaces}` },
    ]);
    expect(performance.now() - started).toBeLessThan(1000);
  });

  it('clips cell content by default and wraps when asked', () => {
    renderTable([
      { id: 'name', header: 'Name', cell: (r) => r.name },
      { id: 'status', header: 'Status', cell: (r) => r.status, wrap: true },
    ]);
    expect(screen.getByRole('cell', { name: 'apollo' })).toHaveClass(
      'overflow-hidden',
      'text-ellipsis',
      'whitespace-nowrap',
    );
    expect(screen.getByRole('cell', { name: 'healthy' })).toHaveClass('whitespace-normal');
  });
});

describe('DataTable searchable multi-select filter', () => {
  interface Item {
    name: string;
    scopes: string[];
  }
  const items: Item[] = [
    { name: 'a', scopes: ['inference:invoke', 'agents:runs:read'] },
    { name: 'b', scopes: ['platform:grants:write'] },
    { name: 'c', scopes: ['mcp:github:read'] },
  ];
  const scopes = [
    'agents:runs:read',
    'inference:invoke',
    'mcp:github:read',
    'platform:grants:write',
  ];

  function renderFiltered() {
    return render(
      <DataTable
        data={items}
        getRowKey={(r) => r.name}
        columns={[
          { id: 'name', header: 'Name', cell: (r) => r.name },
          {
            id: 'scopes',
            header: 'Scopes',
            cell: (r) => r.scopes.join(','),
            filter: {
              mode: 'client',
              searchable: true,
              mono: true,
              options: scopes.map((s) => ({ value: s, label: s })),
              filterFn: (r, selected) => r.scopes.some((s) => selected.has(s)),
            },
          },
        ]}
      />,
    );
  }

  it('narrows the option list with the search box', async () => {
    const user = userEvent.setup();
    renderFiltered();
    await user.click(screen.getByRole('button', { name: 'Filter' }));
    await user.type(screen.getByRole('textbox', { name: 'Search filter options' }), 'github');
    const options = within(screen.getByRole('group', { name: 'Filter options' }));
    expect(options.getAllByRole('button')).toHaveLength(1);
    expect(options.getByRole('button', { name: /mcp:github:read/ })).toBeInTheDocument();
  });

  it('keeps rows matching ANY selected scope (union)', async () => {
    const user = userEvent.setup();
    renderFiltered();
    await user.click(screen.getByRole('button', { name: 'Filter' }));
    const options = within(screen.getByRole('group', { name: 'Filter options' }));
    await user.click(options.getByRole('button', { name: /agents:runs:read/ }));
    await user.click(options.getByRole('button', { name: /mcp:github:read/ }));
    expect(options.getByRole('button', { name: /mcp:github:read/ })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    const names = screen
      .getAllByRole('row')
      .slice(1)
      .map((r) => r.textContent ?? '');
    expect(names).toHaveLength(2);
    expect(names.some((t) => t.startsWith('a'))).toBe(true);
    expect(names.some((t) => t.startsWith('c'))).toBe(true);
    expect(names.some((t) => t.startsWith('b'))).toBe(false);
  });
});

describe('DataTable horizontal scrolling', () => {
  const columns = (): DataTableColumn<Row>[] => [
    { id: 'name', header: 'Name', cell: (r) => r.name, gridWidth: '10rem', minWidth: '8rem' },
    {
      id: 'status',
      header: 'Status',
      cell: (r) => r.status,
      gridWidth: 'minmax(0, 2fr)',
      minWidth: '6rem',
    },
    { id: 'rank', header: 'Rank', cell: (r) => r.rank, gridWidth: '48px' },
  ];

  it("sizes the scrolling region to the columns' minimum widths", () => {
    expect(tableMinWidth(columns())).toBe('calc(8rem + 6rem + 48px + 60px)'); // 2 gaps x 14 + 32
  });

  it('scrolls the header and body together, not the page', () => {
    renderTable(columns());
    const scroller = screen.getByRole('table').querySelector('.overflow-x-auto') as HTMLElement;
    expect(scroller).not.toBeNull();
    expect(scroller).toContainElement(screen.getAllByRole('row')[0]);
    expect(scroller).toContainElement(screen.getByRole('cell', { name: 'apollo' }));
    expect((scroller.firstElementChild as HTMLElement).style.minWidth).toBe(
      'calc(8rem + 6rem + 48px + 60px)',
    );
  });

  it('never hides a column because the table is narrow', () => {
    renderTable(columns());
    expect(screen.getAllByRole('columnheader')).toHaveLength(3);
  });
});

describe('DataTable column titles', () => {
  it('draws every title in capitals, including sortable ones, whatever its casing', () => {
    renderTable([
      { id: 'plain', header: 'Mixed Case', cell: (r) => r.name },
      {
        id: 'sortable',
        header: 'sortable title',
        cell: (r) => r.rank,
        sortable: true,
        sortAccessor: (r) => r.rank,
      },
    ]);
    const [plain, sortable] = screen.getAllByRole('columnheader');
    // The header row carries the overline treatment (uppercase) ...
    expect(screen.getAllByRole('row')[0]).toHaveClass('uppercase');
    expect(plain.textContent).toBe('Mixed Case');
    // ... and the sort button, which Tailwind's preflight would otherwise reset to
    // normal case, is told to inherit it.
    const button = sortable.querySelector('button') as HTMLElement;
    expect(button.className).toContain('[text-transform:inherit]');
    expect(button.className).toContain('[font:inherit]');
  });
});

describe('DataTable pinned-end column and scroll affordance', () => {
  const columns = (): DataTableColumn<Row>[] => [
    { id: 'name', header: 'Name', cell: (r) => r.name, locked: true },
    { id: 'status', header: 'Status', cell: (r) => r.status },
    { id: 'actions', header: '', cell: () => 'del', gridWidth: '3rem', pinEnd: true },
  ];

  it('pins the pinEnd column to the right edge, header and cells, with an opaque background', () => {
    renderTable(columns());
    const [, , actionsHeader] = screen.getAllByRole('columnheader');
    expect(actionsHeader).toHaveClass('sticky', 'right-0', 'z-10', 'bg-surface');
    expect(screen.getByRole('cell', { name: 'del' })).toHaveClass(
      'sticky',
      'right-0',
      'z-10',
      'bg-surface',
    );
  });

  it('does not pin any other column to the right', () => {
    renderTable(columns());
    expect(screen.getByRole('cell', { name: 'healthy' }).className).not.toContain('right-0');
    expect(screen.getByRole('columnheader', { name: 'Status' }).className).not.toContain('right-0');
  });

  it('pins nothing to the right when no column asks for it', () => {
    renderTable([
      { id: 'name', header: 'Name', cell: (r) => r.name },
      { id: 'status', header: 'Status', cell: (r) => r.status },
    ]);
    expect(screen.getByRole('cell', { name: 'apollo' }).className).not.toContain('right-0');
  });

  it('draws a persistent scrollbar on the scroll region instead of the hidden overlay one', () => {
    renderTable(columns());
    const scroller = screen.getByRole('table').querySelector('.overflow-x-auto') as HTMLElement;
    expect(scroller.className).toContain('[&::-webkit-scrollbar]:h-2.5');
    expect(scroller.className).toContain('[&::-webkit-scrollbar-thumb]:bg-border');
  });
});

describe('DataTable pinned-column elevation', () => {
  const OriginalRO = globalThis.ResizeObserver;
  const dims = { scrollWidth: 900, clientWidth: 500 };

  afterEach(() => {
    globalThis.ResizeObserver = OriginalRO;
    vi.restoreAllMocks();
  });

  function renderScrollable(content = dims) {
    // jsdom has no layout: give the scroll region a content wider than its viewport.
    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockReturnValue(content.scrollWidth);
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(content.clientWidth);
    globalThis.ResizeObserver = class {
      constructor(private cb: ResizeObserverCallback) {}
      observe() {
        this.cb([], this as never);
      }
      disconnect() {}
      unobserve() {}
    } as unknown as typeof ResizeObserver;
    return renderTable([
      { id: 'name', header: 'Name', cell: (r) => r.name, locked: true },
      { id: 'status', header: 'Status', cell: (r) => r.status },
      { id: 'actions', header: '', cell: () => 'del', pinEnd: true },
    ]);
  }

  const scroller = () => screen.getByRole('table').querySelector('.overflow-x-auto') as HTMLElement;

  it('elevates only the right pin while there is more to scroll to the right', () => {
    renderScrollable();
    const actions = screen.getByRole('cell', { name: 'del' });
    const name = screen.getByRole('cell', { name: 'apollo' });
    expect(actions).toHaveClass(
      'border-border-subtle',
      'bg-surface-elevated',
      'shadow-[-8px_0_12px_-6px_rgb(0_0_0/0.8)]',
    );
    // Light theme swaps the tone back and softens the shadow.
    expect(actions.className).toContain(
      '[[data-theme=light]_&]:shadow-[-6px_0_8px_-6px_rgb(0_0_0/0.15)]',
    );
    expect(actions.className).toContain('[[data-theme=light]_&]:bg-surface');
    expect(actions).not.toHaveClass('border-transparent');
    // At the start nothing is under the left pin, so it stays flat.
    expect(name).toHaveClass('border-transparent');
    expect(name).not.toHaveClass('border-border-subtle');
  });

  it('elevates the left pin once scrolled away from the start, and drops the right pin at the end', () => {
    renderScrollable();
    const el = scroller();
    el.scrollLeft = 400; // 400 + 500 viewport = 900: scrolled all the way right
    fireEvent.scroll(el);
    expect(screen.getByRole('cell', { name: 'apollo' })).toHaveClass(
      'border-border-subtle',
      'shadow-[8px_0_12px_-6px_rgb(0_0_0/0.8)]',
    );
    const actions = screen.getByRole('cell', { name: 'del' });
    expect(actions).toHaveClass('border-transparent');
    expect(actions).not.toHaveClass('border-border-subtle');
  });

  it('elevates both pins when scrolled to the middle', () => {
    renderScrollable();
    const el = scroller();
    el.scrollLeft = 150;
    fireEvent.scroll(el);
    expect(screen.getByRole('cell', { name: 'apollo' })).toHaveClass('border-border-subtle');
    expect(screen.getByRole('cell', { name: 'del' })).toHaveClass('border-border-subtle');
  });

  it('never elevates anything when the content fits', () => {
    renderScrollable({ scrollWidth: 500, clientWidth: 500 });
    expect(screen.getByRole('cell', { name: 'del' })).toHaveClass('border-transparent');
    expect(screen.getByRole('cell', { name: 'apollo' })).toHaveClass('border-transparent');
  });

  it('elevates the pinned header cells the same way', () => {
    renderScrollable();
    const [nameHeader, , actionsHeader] = screen.getAllByRole('columnheader');
    expect(actionsHeader).toHaveClass('border-border-subtle');
    expect(nameHeader).toHaveClass('border-transparent');
  });

  it('centres the actions content between the edge line and the table edge', () => {
    renderScrollable();
    // Padding is split evenly either side of the content, so with the line showing
    // the icon sits in the middle of the pinned column instead of hugging the line.
    expect(screen.getByRole('cell', { name: 'del' })).toHaveClass('justify-center', 'pl-2', 'pr-2');
  });
});
