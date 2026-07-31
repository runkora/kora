import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Badge } from '../badge/badge';
import { Button } from '../button/button';
import { Icon } from '../icon';
import { Input } from '../input';
import { Sparkline } from '../sparkline';
import { StatusDot } from '../status-dot';
import { Text } from '../typography';
import {
  DataGrid,
  DataGridEmpty,
  DataGridFooter,
  DataGridHeader,
  DataGridRow,
  DataGridToolbar,
} from './data-grid';
import { DataGridPagination } from './data-grid-pagination';

const meta: Meta<typeof DataGrid> = {
  title: 'Terra/Organisms/DataGrid',
  component: DataGrid,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof DataGrid>;

const COLUMNS = '1fr 132px 120px 88px';

interface Row {
  id: string;
  name: string;
  kind: string;
  fires: number[];
  healthy: boolean;
}

const ROWS: Row[] = [
  { id: '1', name: 'pr-review', kind: 'webhook', fires: [3, 5, 2, 8, 6, 9, 4], healthy: true },
  {
    id: '2',
    name: 'nightly-audit',
    kind: 'scheduled',
    fires: [1, 1, 1, 1, 1, 1, 1],
    healthy: true,
  },
  {
    id: '3',
    name: 'incident-triage',
    kind: 'manual',
    fires: [0, 2, 0, 0, 7, 1, 0],
    healthy: false,
  },
  { id: '4', name: 'dep-bump', kind: 'scheduled', fires: [2, 2, 3, 2, 2, 4, 2], healthy: true },
];

function Cells({ row }: { row: Row }) {
  return (
    <>
      <Text size="caption" weight="emphasis" truncate>
        {row.name}
      </Text>
      <Badge variant="dim" size="sm">
        {row.kind}
      </Badge>
      <Sparkline values={row.fires} />
      <StatusDot status={row.healthy ? 'healthy' : 'error'} />
    </>
  );
}

/**
 * The whole family: a toolbar, a header taking its labels as data, rows sharing
 * the root's column template, and a footer — all inside one frame.
 */
export const Default: Story = {
  render: () => (
    <div className="w-[680px]">
      <DataGrid columns={COLUMNS}>
        <DataGridToolbar>
          <Input compact placeholder="Search triggers…" aria-label="Search triggers" />
          <span className="grow" />
          <Button variant="ghost" size="xs">
            <Icon name="filter" size="2xs" />
            Filter
          </Button>
        </DataGridToolbar>
        <DataGridHeader
          cells={['Trigger', 'Kind', 'Fires (7d)', { label: 'Health', align: 'end' }]}
        />
        {ROWS.map((row) => (
          <DataGridRow key={row.id}>
            <Cells row={row} />
          </DataGridRow>
        ))}
        <DataGridFooter>
          <Text size="caption" color="subtle">
            4 triggers
          </Text>
        </DataGridFooter>
      </DataGrid>
    </div>
  ),
};

/**
 * Rows only. The last row's bottom border is derived in CSS, so the frame
 * closes cleanly without anything being told which row is last.
 */
export const RowsOnly: Story = {
  render: () => (
    <div className="w-[680px]">
      <DataGrid columns={COLUMNS}>
        <DataGridHeader
          cells={['Trigger', 'Kind', 'Fires (7d)', { label: 'Health', align: 'end' }]}
        />
        {ROWS.map((row) => (
          <DataGridRow key={row.id}>
            <Cells row={row} />
          </DataGridRow>
        ))}
      </DataGrid>
    </div>
  ),
};

/**
 * A clickable row gains a hover surface, a tab stop and Enter/Space activation,
 * so the list is never a mouse-only surface. Give it a `rowLabel` naming what
 * activating it does.
 */
export const ClickableRows: Story = {
  render: () => (
    <div className="w-[680px]">
      <DataGrid columns={COLUMNS}>
        <DataGridHeader
          cells={['Trigger', 'Kind', 'Fires (7d)', { label: 'Health', align: 'end' }]}
        />
        {ROWS.map((row) => (
          <DataGridRow
            key={row.id}
            rowLabel={`Open ${row.name}`}
            onClick={() => window.alert(`Open ${row.name}`)}
          >
            <Cells row={row} />
          </DataGridRow>
        ))}
      </DataGrid>
    </div>
  ),
};

/** Nothing to show — inside the frame, so the filter that emptied it stays reachable. */
export const Empty: Story = {
  render: () => (
    <div className="w-[680px]">
      <DataGrid columns={COLUMNS}>
        <DataGridToolbar>
          <Input compact defaultValue="zzz" aria-label="Search triggers" />
        </DataGridToolbar>
        <DataGridHeader
          cells={['Trigger', 'Kind', 'Fires (7d)', { label: 'Health', align: 'end' }]}
        />
        <DataGridEmpty>
          <Icon name="search-x" size="sm" color="subtle" />
          <Text size="supporting" color="muted">
            No triggers match “zzz”.
          </Text>
        </DataGridEmpty>
      </DataGrid>
    </div>
  ),
};

/**
 * `DataGridPagination` is the standard footer. It is presentational: the
 * consumer owns the paging arithmetic and passes the computed window in.
 */
export const Paginated: Story = {
  render: function Render() {
    const [page, setPage] = React.useState(0);
    const [pageSize, setPageSize] = React.useState(2);
    const pageCount = Math.ceil(ROWS.length / pageSize);
    const visible = ROWS.slice(page * pageSize, page * pageSize + pageSize);
    return (
      <div className="w-[680px]">
        <DataGrid columns={COLUMNS}>
          <DataGridHeader
            cells={['Trigger', 'Kind', 'Fires (7d)', { label: 'Health', align: 'end' }]}
          />
          {visible.map((row) => (
            <DataGridRow key={row.id}>
              <Cells row={row} />
            </DataGridRow>
          ))}
          <DataGridPagination
            from={page * pageSize + 1}
            to={Math.min((page + 1) * pageSize, ROWS.length)}
            total={ROWS.length}
            page={page}
            pageCount={pageCount}
            pageSize={pageSize}
            pageSizeOptions={[2, 5, 10]}
            onPageChange={setPage}
            onPageSizeChange={(size) => {
              setPageSize(size);
              setPage(0);
            }}
            noun="triggers"
          />
        </DataGrid>
      </div>
    );
  },
};
