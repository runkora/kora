'use client';

import type { Meta, StoryObj } from '@storybook/react';
import { DataTable } from './index';
import type { TableColumnFilter, DataTableColumn } from './index';
import { Badge, Card, CardContent, CardFooter, CardHeader, Stack, Text } from '../../index';

// ── Meta ──────────────────────────────────────────────────────────────────────

const meta: Meta<typeof DataTable> = {
  title: 'Terra/Organisms/DataTable',
  component: DataTable,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
  argTypes: {
    // ── Required props ─────────────────────────────────────────────────────
    data: {
      control: false,
      description: 'Dataset to display. Pass the full client-side array or a single server page.',
    },
    columns: {
      control: false,
      description:
        'Column definitions. Each entry controls header label, cell renderer, sort, filter, and visibility.',
    },
    getRowKey: {
      control: false,
      description: 'Extract a stable React key from a row.',
    },
    // ── Controllable optional props ────────────────────────────────────────
    noun: {
      control: 'text',
      description: 'Row noun used in the "Showing X of Y" label.',
      table: { defaultValue: { summary: 'records' } },
    },
    searchPlaceholder: {
      control: 'text',
      description:
        'Placeholder text for the search input. Must be paired with `searchFn`; omit both to hide the search box.',
    },
    defaultSortId: {
      control: 'text',
      description: 'Column `id` to sort by on initial render.',
    },
    defaultSortDir: {
      control: 'radio',
      options: ['asc', 'desc'],
      description: 'Initial sort direction.',
      table: { defaultValue: { summary: 'asc' } },
    },
    emptyMessage: {
      control: 'text',
      description: 'Message shown when the dataset is empty and no filters are active.',
      table: { defaultValue: { summary: 'No records found.' } },
    },
    filteredEmptyMessage: {
      control: 'text',
      description: 'Message shown when search or filters produce zero results.',
      table: { defaultValue: { summary: 'No records match your filters.' } },
    },
    totalCount: {
      control: { type: 'number', min: 0 },
      description:
        'Override the total shown in "Showing X of Y". Use for server-paginated tables where `data` is a single page. Avoid combining with client-side `searchFn` — filters reduce `showing` independently.',
    },
    rowClickAriaLabel: {
      control: 'text',
      description:
        'Visually-hidden `sr-only` description announced by screen readers when a clickable row receives focus. Not visible in the UI.',
      table: { defaultValue: { summary: 'View details' } },
    },
    // ── Complex optional props — disabled controls, documented ────────────
    searchFn: {
      control: false,
      description:
        'Full-text search predicate. Must be paired with `searchPlaceholder`. Return true if the row matches.',
    },
    error: {
      control: false,
      description:
        'ReactNode rendered in place of rows when an error occurs. Suppresses empty state and row list.',
    },
    toolbarActions: {
      control: false,
      description:
        'Extra content rendered in the toolbar after the search input, before the count/columns section.',
    },
    onRowClick: {
      control: false,
      description: 'Called when a data row is clicked. Adds pointer cursor to rows.',
    },
    grid: {
      control: false,
      description:
        'Card grid config. `card` renders a row as a card; `footer` is a static trailing node (e.g. "Create new") — `onRowClick` does NOT wrap it; `cols` sets grid columns; `defaultViewMode` sets the initial desktop view.',
    },
  },
};

export default meta;
type Story = StoryObj<typeof DataTable>;

// ── Fixtures ──────────────────────────────────────────────────────────────────

const agents = [
  { id: 'apollo-3', status: 'Running', tools: 3, uptime: '4d 12h', cpu: '42%' },
  { id: 'hermes-1', status: 'Idle', tools: 0, uptime: '2d 3h', cpu: '1%' },
  { id: 'minerva-2', status: 'Suspended', tools: 1, uptime: '0d 8h', cpu: '0%' },
  { id: 'daedalus-4', status: 'Error', tools: 2, uptime: '1d 1h', cpu: '89%' },
  { id: 'icarus-5', status: 'Running', tools: 4, uptime: '6d 22h', cpu: '61%' },
];

const STATUS_OPTS: TableColumnFilter['options'] = [
  { value: 'Running', label: 'Running' },
  { value: 'Idle', label: 'Idle' },
  { value: 'Suspended', label: 'Suspended' },
  { value: 'Error', label: 'Error' },
];

type Agent = (typeof agents)[number];

const BASE_COLUMNS: DataTableColumn<Agent>[] = [
  {
    id: 'id',
    header: 'Agent ID',
    locked: true,
    sortable: true,
    sortAccessor: (a) => a.id,
    cell: (a) => <code>{a.id}</code>,
  },
  {
    id: 'status',
    header: 'Status',
    filter: {
      mode: 'client' as const,
      options: STATUS_OPTS,
      filterFn: (a, selected) => selected.has(a.status),
    },
    cell: (a) => a.status,
  },
  {
    id: 'tools',
    header: 'Tools',
    sortable: true,
    sortAccessor: (a) => String(a.tools),
    cell: (a) => a.tools,
  },
  {
    id: 'uptime',
    header: 'Uptime',
    cell: (a) => a.uptime,
  },
  {
    id: 'cpu',
    header: 'CPU',
    align: 'right' as const,
    sortable: true,
    defaultVisible: false,
    sortAccessor: (a) => a.cpu,
    cell: (a) => a.cpu,
  },
];

// Shared card renderers used across grid stories.
function AgentCard({ agent }: { agent: Agent }) {
  return (
    <Card column style={{ height: '100%', cursor: 'pointer' }}>
      <CardHeader
        leadingIcon="bot"
        title={agent.id}
        description={
          <Text mono size="caption" color="muted">
            {agent.status}
          </Text>
        }
        divider
      />
      <CardContent padded grow>
        <Stack gap="snug">
          <Text size="supporting" color="muted">
            CPU: {agent.cpu}
          </Text>
          <Stack direction="horizontal" gap="tight" wrap>
            <Badge variant="secondary" size="sm">
              {agent.tools} tools
            </Badge>
            <Badge variant="outline" size="sm">
              {agent.uptime}
            </Badge>
          </Stack>
        </Stack>
      </CardContent>
      <CardFooter compact justify="between">
        <Text size="caption" color="muted">
          Uptime {agent.uptime}
        </Text>
      </CardFooter>
    </Card>
  );
}

const newAgentCard = (
  <Card
    column
    style={{ height: '100%', cursor: 'pointer', background: 'transparent', borderStyle: 'dashed' }}
  >
    <CardHeader
      leadingIcon="bot"
      title="placeholder"
      description="placeholder"
      divider
      style={{ visibility: 'hidden' }}
      aria-hidden="true"
    />
    <CardContent padded grow>
      <Stack align="center" justify="center" grow gap="tight">
        <Text size="supporting" color="muted">
          + New agent
        </Text>
      </Stack>
    </CardContent>
    <CardFooter compact justify="between" style={{ visibility: 'hidden' }} aria-hidden="true">
      <Text size="caption" color="muted">
        placeholder
      </Text>
    </CardFooter>
  </Card>
);

// ── Stories ───────────────────────────────────────────────────────────────────

// 1. Canonical — every feature on: search, sort, filter, column visibility toggle, grid/list toggle
export const Canonical: Story = {
  name: 'Canonical — all features',
  args: {
    noun: 'agents',
    searchPlaceholder: 'Search agents…',
    defaultSortId: 'id',
    defaultSortDir: 'asc',
    emptyMessage: 'No agents found.',
    filteredEmptyMessage: 'No agents match your filters.',
  },
  render: (args) => (
    <div className="w-[860px]">
      <DataTable
        {...args}
        data={agents}
        columns={[
          {
            id: 'id',
            header: 'Agent ID',
            locked: true,
            sortable: true,
            sortAccessor: (a) => a.id,
            cell: (a) => <code>{a.id}</code>,
          },
          {
            id: 'status',
            header: 'Status',
            locked: true,
            filter: {
              mode: 'client',
              options: STATUS_OPTS,
              filterFn: (a, selected) => selected.has(a.status),
            },
            cell: (a) => a.status,
          },
          {
            id: 'tools',
            header: 'Tools',
            sortable: true,
            sortAccessor: (a) => String(a.tools),
            cell: (a) => a.tools,
          },
          {
            id: 'uptime',
            header: 'Uptime',
            cell: (a) => a.uptime,
          },
          {
            id: 'cpu',
            header: 'CPU',
            align: 'right',
            sortable: true,
            defaultVisible: false,
            sortAccessor: (a) => a.cpu,
            cell: (a) => a.cpu,
          },
        ]}
        getRowKey={(a) => a.id}
        searchFn={(row, q) =>
          row.id.toLowerCase().includes(q) || row.status.toLowerCase().includes(q)
        }
        grid={{
          cols: { base: 1, sm: 2, lg: 3 },
          card: (agent) => <AgentCard agent={agent} />,
          footer: newAgentCard,
        }}
      />
    </div>
  ),
};

// 2. Table-only view — no grid, so no toggle (column visibility still present)
export const TableView: Story = {
  name: 'Table view — sort + filter + columns',
  args: {
    noun: 'agents',
    searchPlaceholder: 'Search agents…',
    defaultSortId: 'id',
    defaultSortDir: 'asc',
    emptyMessage: 'No agents found.',
    filteredEmptyMessage: 'No agents match your filters.',
  },
  render: (args) => (
    <div className="w-[720px]">
      <DataTable
        {...args}
        data={agents}
        columns={BASE_COLUMNS}
        getRowKey={(a) => a.id}
        searchFn={(row, q) =>
          row.id.toLowerCase().includes(q) || row.status.toLowerCase().includes(q)
        }
      />
    </div>
  ),
};

// 3. Grid view — grid config + built-in desktop toggle
export const GridView: Story = {
  name: 'Grid view — grid config + toggle',
  args: {
    noun: 'agents',
    searchPlaceholder: 'Search agents…',
    defaultSortId: 'id',
    defaultSortDir: 'asc',
    emptyMessage: 'No agents found.',
    filteredEmptyMessage: 'No agents match your filters.',
  },
  render: (args) => (
    <div className="w-[860px]">
      <DataTable
        {...args}
        data={agents}
        columns={BASE_COLUMNS.map((c) => ({ ...c, locked: true }))}
        getRowKey={(a) => a.id}
        searchFn={(row, q) =>
          row.id.toLowerCase().includes(q) || row.status.toLowerCase().includes(q)
        }
        grid={{
          cols: { base: 1, sm: 2, lg: 3 },
          card: (agent) => <AgentCard agent={agent} />,
          footer: newAgentCard,
        }}
      />
    </div>
  ),
};

// 4. Empty state
export const Empty: Story = {
  name: 'Empty state',
  args: {
    noun: 'agents',
    searchPlaceholder: 'Search agents…',
    defaultSortId: 'id',
    emptyMessage: 'No agents yet.',
    filteredEmptyMessage: 'No agents match your search.',
  },
  render: (args) => (
    <div className="w-[720px]">
      <DataTable
        {...args}
        data={[]}
        columns={BASE_COLUMNS}
        getRowKey={(a) => a.id}
        searchFn={(row, q) => row.id.toLowerCase().includes(q)}
      />
    </div>
  ),
};

// 5. Error state
export const ErrorState: Story = {
  name: 'Error state',
  args: {
    noun: 'agents',
    searchPlaceholder: 'Search agents…',
    defaultSortId: 'id',
    emptyMessage: 'No agents yet.',
    filteredEmptyMessage: 'No agents match your search.',
  },
  render: (args) => (
    <div className="w-[720px]">
      <DataTable
        {...args}
        data={[]}
        columns={BASE_COLUMNS}
        getRowKey={(a) => a.id}
        searchFn={(row, q) => row.id.toLowerCase().includes(q)}
        error={
          <Stack align="center" gap="snug" style={{ padding: '1.5rem' }}>
            <Text size="supporting" color="muted">
              Failed to load agents.
            </Text>
          </Stack>
        }
      />
    </div>
  ),
};

// 6. toolbarActions — extra content left of the count (e.g. a CTA button)
export const WithToolbarActions: Story = {
  name: 'With toolbarActions',
  args: {
    noun: 'agents',
    searchPlaceholder: 'Search agents…',
    defaultSortId: 'id',
    emptyMessage: 'No agents found.',
    filteredEmptyMessage: 'No agents match your filters.',
  },
  render: (args) => (
    <div className="w-[720px]">
      <DataTable
        {...args}
        data={agents}
        columns={BASE_COLUMNS}
        getRowKey={(a) => a.id}
        searchFn={(row, q) =>
          row.id.toLowerCase().includes(q) || row.status.toLowerCase().includes(q)
        }
        toolbarActions={
          <button
            type="button"
            className="inline-flex items-center gap-1.5 rounded-md border border-border bg-surface px-3 py-1.5 text-sm font-medium text-foreground shadow-sm transition-colors hover:bg-surface-elevated"
          >
            + New agent
          </button>
        }
      />
    </div>
  ),
};
