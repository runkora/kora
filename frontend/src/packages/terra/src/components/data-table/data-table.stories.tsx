'use client';

/**
 * Stories for Terra/Organisms/DataTable.
 *
 * DataTable is the opinionated CSS-grid list component that replaces both the
 * old DataGrid (layout-only) and the old DataTable (TanStack + <table> elements).
 * Visual language: flat rows, hairline separators, rounded-lg bordered container.
 *
 * ## When to use DataTable vs SmartTable
 *
 * ```
 * Are you building inside (dashboard)/ (authenticated shell)?
 * ├── No  → use DataTable from @orbit/terra
 * └── Yes → Does this table need column visibility persisted per user?
 *           ├── No  → use DataTable from @orbit/terra
 *           └── Yes → use SmartTable from @orbit/components  ← default choice
 * ```
 */

import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Badge } from '../badge';
import { Stack } from '../layout';
import { Text } from '../typography';
import { Button } from '../button';
import { DataTable } from './index';

// ── Meta ──────────────────────────────────────────────────────────────────────

const meta: Meta<typeof DataTable> = {
  title: 'Terra/Organisms/DataTable',
  component: DataTable,
  parameters: {
    layout: 'padded',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof DataTable>;

// ── Fixtures ──────────────────────────────────────────────────────────────────

interface AgentRow {
  id: string;
  status: 'Running' | 'Idle' | 'Suspended';
  tools: number;
  uptime: string;
  cost: number;
}

const AGENTS: AgentRow[] = [
  { id: 'apollo-3', status: 'Running', tools: 3, uptime: '4d 12h', cost: 12.5 },
  { id: 'hermes-1', status: 'Idle', tools: 0, uptime: '2d 3h', cost: 0.3 },
  { id: 'minerva-2', status: 'Suspended', tools: 1, uptime: '0d 8h', cost: 4.1 },
  { id: 'zeus-7', status: 'Running', tools: 5, uptime: '1d 1h', cost: 7.2 },
  { id: 'athena-4', status: 'Idle', tools: 2, uptime: '6d 0h', cost: 1.1 },
  { id: 'hera-9', status: 'Suspended', tools: 0, uptime: '3d 4h', cost: 0.0 },
];

const STATUS_OPTS = [
  { value: 'Running', label: 'Running', dot: 'bg-status-healthy' },
  { value: 'Idle', label: 'Idle', dot: 'bg-status-degraded' },
  { value: 'Suspended', label: 'Suspended', dot: 'bg-status-error' },
];

// ── 1. Basic ──────────────────────────────────────────────────────────────────

export const Basic: Story = {
  name: 'Basic — no sort or filter',
  render: () => (
    <div className="w-[700px]">
      <DataTable
        data={AGENTS}
        columns={[
          {
            id: 'id',
            header: 'Agent ID',
            gridWidth: '1fr',
            locked: true,
            cell: (a) => (
              <Text size="supporting" mono>
                {a.id}
              </Text>
            ),
          },
          {
            id: 'status',
            header: 'Status',
            gridWidth: '120px',
            cell: (a) => <Badge variant="outline">{a.status}</Badge>,
          },
          {
            id: 'tools',
            header: 'Tools',
            gridWidth: '80px',
            align: 'right',
            cell: (a) => (
              <Text size="supporting" mono align="end">
                {a.tools}
              </Text>
            ),
          },
          {
            id: 'uptime',
            header: 'Uptime',
            gridWidth: '100px',
            cell: (a) => (
              <Text size="supporting" mono>
                {a.uptime}
              </Text>
            ),
          },
        ]}
        getRowKey={(a) => a.id}
        noun="agents"
        emptyMessage="No agents found."
      />
    </div>
  ),
};

// ── 2. Sort + filter + search ─────────────────────────────────────────────────

export const FullFeatured: Story = {
  name: 'Sort + filter + search + column visibility',
  render: () => (
    <div className="w-[700px]">
      <DataTable
        data={AGENTS}
        columns={[
          {
            id: 'id',
            header: 'Agent ID',
            gridWidth: '1fr',
            locked: true,
            sortable: true,
            sortAccessor: (a) => a.id,
            cell: (a) => (
              <Text size="supporting" mono weight="emphasis">
                {a.id}
              </Text>
            ),
          },
          {
            id: 'status',
            header: 'Status',
            gridWidth: '140px',
            filter: {
              mode: 'client',
              options: STATUS_OPTS,
              filterFn: (a, selected) => selected.has(a.status),
            },
            cell: (a) => <Badge variant="outline">{a.status}</Badge>,
          },
          {
            id: 'tools',
            header: 'Tools',
            gridWidth: '80px',
            align: 'right',
            sortable: true,
            sortAccessor: (a) => a.tools,
            cell: (a) => (
              <Text size="supporting" mono align="end">
                {a.tools}
              </Text>
            ),
          },
          {
            id: 'uptime',
            header: 'Uptime',
            gridWidth: '100px',
            cell: (a) => (
              <Text size="supporting" mono>
                {a.uptime}
              </Text>
            ),
          },
          {
            id: 'cost',
            header: 'Cost',
            gridWidth: '90px',
            align: 'right',
            sortable: true,
            defaultVisible: false,
            sortAccessor: (a) => a.cost,
            cell: (a) => (
              <Text size="supporting" mono align="end">
                ${a.cost.toFixed(2)}
              </Text>
            ),
          },
        ]}
        getRowKey={(a) => a.id}
        noun="agents"
        searchPlaceholder="Search agents…"
        searchFn={(a, q) => a.id.toLowerCase().includes(q) || a.status.toLowerCase().includes(q)}
        defaultSortId="id"
        defaultSortDir="asc"
        emptyMessage="No agents found."
        filteredEmptyMessage="No agents match your filters."
      />
    </div>
  ),
};

// ── 3. Clickable rows ─────────────────────────────────────────────────────────

function ClickableRowsDemo() {
  const [selected, setSelected] = useState<string | null>(null);
  return (
    <Stack gap="comfortable" style={{ width: '600px' }}>
      {selected && (
        <Text size="supporting" color="muted">
          Last clicked:{' '}
          <Text as="span" mono>
            {selected}
          </Text>
        </Text>
      )}
      <DataTable
        data={AGENTS}
        columns={[
          {
            id: 'id',
            header: 'Agent ID',
            gridWidth: '1fr',
            locked: true,
            cell: (a) => (
              <Text size="supporting" mono weight="emphasis">
                {a.id}
              </Text>
            ),
          },
          {
            id: 'status',
            header: 'Status',
            gridWidth: '120px',
            cell: (a) => <Badge variant="outline">{a.status}</Badge>,
          },
        ]}
        getRowKey={(a) => a.id}
        noun="agents"
        emptyMessage="No agents."
        onRowClick={(a) => setSelected(a.id)}
        rowLabel={(a) => `Open agent ${a.id}`}
      />
    </Stack>
  );
}

export const ClickableRows: Story = {
  name: 'Clickable rows',
  render: () => <ClickableRowsDemo />,
};

// ── 4. Client-side pagination ─────────────────────────────────────────────────

export const Paginated: Story = {
  name: 'Client-side pagination',
  render: () => {
    // Generate a longer dataset
    const rows: AgentRow[] = Array.from({ length: 30 }, (_, i) => ({
      id: `agent-${String(i + 1).padStart(2, '0')}`,
      status: (['Running', 'Idle', 'Suspended'] as const)[i % 3],
      tools: i % 6,
      uptime: `${i}d ${i % 24}h`,
      cost: +(i * 1.5).toFixed(2),
    }));

    return (
      <div className="w-[700px]">
        <DataTable
          data={rows}
          columns={[
            {
              id: 'id',
              header: 'Agent ID',
              gridWidth: '1fr',
              locked: true,
              cell: (a) => (
                <Text size="supporting" mono>
                  {a.id}
                </Text>
              ),
            },
            {
              id: 'status',
              header: 'Status',
              gridWidth: '120px',
              cell: (a) => <Badge variant="outline">{a.status}</Badge>,
            },
            {
              id: 'cost',
              header: 'Cost',
              gridWidth: '90px',
              align: 'right',
              cell: (a) => (
                <Text size="supporting" mono align="end">
                  ${a.cost.toFixed(2)}
                </Text>
              ),
            },
          ]}
          getRowKey={(a) => a.id}
          noun="agents"
          emptyMessage="No agents."
          pagination={{ pageSizeOptions: [5, 10, 25], defaultPageSize: 5 }}
        />
      </div>
    );
  },
};

// ── 5. Toolbar actions slot ───────────────────────────────────────────────────

export const ToolbarActions: Story = {
  name: 'Toolbar actions slot',
  render: () => (
    <div className="w-[700px]">
      <DataTable
        data={AGENTS}
        columns={[
          {
            id: 'id',
            header: 'Agent ID',
            gridWidth: '1fr',
            locked: true,
            cell: (a) => (
              <Text size="supporting" mono>
                {a.id}
              </Text>
            ),
          },
          {
            id: 'status',
            header: 'Status',
            gridWidth: '120px',
            cell: (a) => <Badge variant="outline">{a.status}</Badge>,
          },
        ]}
        getRowKey={(a) => a.id}
        noun="agents"
        emptyMessage="No agents."
        toolbarActions={
          <Button variant="outline" size="sm">
            New Agent
          </Button>
        }
      />
    </div>
  ),
};

// ── 6. Empty state ────────────────────────────────────────────────────────────

export const EmptyState: Story = {
  name: 'Empty state',
  render: () => (
    <div className="w-[500px]">
      <DataTable
        data={[]}
        columns={[
          {
            id: 'id',
            header: 'Agent ID',
            gridWidth: '1fr',
            locked: true,
            cell: (a: AgentRow) => (
              <Text size="supporting" mono>
                {a.id}
              </Text>
            ),
          },
          {
            id: 'status',
            header: 'Status',
            gridWidth: '120px',
            cell: (a: AgentRow) => <Badge variant="outline">{a.status}</Badge>,
          },
        ]}
        getRowKey={(a) => a.id}
        emptyMessage="No agents yet. Create one to get started."
      />
    </div>
  ),
};
