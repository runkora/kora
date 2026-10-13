'use client';

import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Badge } from '../badge';
import { Button } from '../button';
import { Stack } from '../layout';
import { SegmentedControl } from '../segmented-control';
import { Text } from '../typography';
import { DataTable } from './index';
import type { DataTableColumn, DataTableFilterOption } from './index';

/**
 * DataTable
 *
 * A full table of records with search, sorting, filters, a column menu and pages. Give it the
 * rows and a list of columns, and it handles the rest.
 */
const meta: Meta<typeof DataTable> = {
  title: 'Hades/Organisms/DataTable',
  component: DataTable,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof DataTable>;

type Status = 'Running' | 'Idle' | 'Failed' | 'Suspended';

interface Agent {
  name: string;
  status: Status;
  provider: string;
  runs: number;
  cost: number;
  lastRun: string;
}

const statusVariant = {
  Running: 'healthy',
  Idle: 'idle',
  Failed: 'error',
  Suspended: 'pending',
} as const;

const statusOptions: DataTableFilterOption[] = [
  { value: 'Running', label: 'Running', dot: 'bg-status-healthy' },
  { value: 'Idle', label: 'Idle', dot: 'bg-status-idle' },
  { value: 'Failed', label: 'Failed', dot: 'bg-status-error' },
  { value: 'Suspended', label: 'Suspended', dot: 'bg-status-pending' },
];

const agents: Agent[] = [
  ['apollo-3', 'Running', 'Anthropic', 412, 41.2, '2 min ago'],
  ['hermes-1', 'Idle', 'Bedrock', 87, 8.7, '3 h ago'],
  ['minerva-2', 'Suspended', 'Vertex AI', 254, 25.4, '5 d ago'],
  ['daedalus-4', 'Failed', 'Anthropic', 63, 6.3, '14 min ago'],
  ['icarus-5', 'Running', 'Bedrock', 329, 32.9, '1 min ago'],
  ['athena-6', 'Running', 'Anthropic', 518, 51.8, '6 min ago'],
  ['hera-7', 'Idle', 'Azure', 42, 4.2, '1 d ago'],
  ['zeus-8', 'Running', 'Anthropic', 903, 90.3, 'Just now'],
  ['ares-9', 'Failed', 'Vertex AI', 119, 11.9, '40 min ago'],
  ['artemis-10', 'Idle', 'Bedrock', 76, 7.6, '2 d ago'],
  ['demeter-11', 'Running', 'Azure', 288, 28.8, '9 min ago'],
  ['hestia-12', 'Suspended', 'Anthropic', 31, 3.1, '9 d ago'],
  ['poseidon-13', 'Running', 'Bedrock', 674, 67.4, '4 min ago'],
  ['apollo-14', 'Idle', 'Anthropic', 95, 9.5, '5 h ago'],
  ['hermes-15', 'Failed', 'Bedrock', 208, 20.8, '1 h ago'],
  ['minerva-16', 'Running', 'Vertex AI', 351, 35.1, '3 min ago'],
  ['dionysus-17', 'Idle', 'Azure', 54, 5.4, '6 h ago'],
  ['hades-18', 'Suspended', 'Anthropic', 12, 1.2, '12 d ago'],
  ['persephone-19', 'Running', 'Bedrock', 447, 44.7, '8 min ago'],
  ['helios-20', 'Running', 'Anthropic', 762, 76.2, '1 min ago'],
  ['selene-21', 'Idle', 'Vertex AI', 68, 6.8, '1 d ago'],
  ['eos-22', 'Failed', 'Anthropic', 173, 17.3, '25 min ago'],
  ['nike-23', 'Running', 'Azure', 296, 29.6, '11 min ago'],
  ['iris-24', 'Idle', 'Bedrock', 81, 8.1, '4 h ago'],
].map(([name, status, provider, runs, cost, lastRun]) => ({
  name: name as string,
  status: status as Status,
  provider: provider as string,
  runs: runs as number,
  cost: cost as number,
  lastRun: lastRun as string,
}));

const usd = (value: number) => `$${value.toFixed(2)}`;

const searchAgents = (agent: Agent, query: string) =>
  agent.name.includes(query) || agent.provider.toLowerCase().includes(query);

const statusFilter = {
  mode: 'client',
  options: statusOptions,
  filterFn: (agent: Agent, selected: Set<string>) => selected.has(agent.status),
} as const;

const nameColumn: DataTableColumn<Agent> = {
  id: 'name',
  header: 'Agent',
  gridWidth: '1fr',
  locked: true,
  cell: (agent) => (
    <Text size="supporting" mono>
      {agent.name}
    </Text>
  ),
};

const statusColumn: DataTableColumn<Agent> = {
  id: 'status',
  header: 'Status',
  gridWidth: '110px',
  cell: (agent) => <Badge variant={statusVariant[agent.status]}>{agent.status}</Badge>,
};

const runsColumn: DataTableColumn<Agent> = {
  id: 'runs',
  header: 'Runs',
  gridWidth: '70px',
  align: 'right',
  cell: (agent) => (
    <Text size="supporting" mono tabular align="end">
      {agent.runs}
    </Text>
  ),
};

const costColumn: DataTableColumn<Agent> = {
  id: 'cost',
  header: 'Cost',
  gridWidth: '80px',
  cell: (agent) => (
    <Text size="supporting" mono tabular>
      {usd(agent.cost)}
    </Text>
  ),
};

const getKey = (agent: Agent) => agent.name;

const sample = agents.slice(0, 6);

function toggleValue(set: Set<string>, value: string) {
  const next = new Set(set);
  if (next.has(value)) next.delete(value);
  else next.add(value);
  return next;
}

function AgentsOverview() {
  const [quick, setQuick] = useState('all');
  const [opened, setOpened] = useState<string | null>(null);

  const rows = quick === 'all' ? agents : agents.filter((a) => a.status.toLowerCase() === quick);
  const count = (status: Status) => agents.filter((a) => a.status === status).length;

  const columns: DataTableColumn<Agent>[] = [
    {
      ...nameColumn,
      sortable: true,
      sortAccessor: (agent) => agent.name,
    },
    { ...statusColumn, filter: statusFilter },
    {
      id: 'provider',
      header: 'Provider',
      gridWidth: '110px',
      defaultVisible: false,
      cell: (agent) => <Text size="supporting">{agent.provider}</Text>,
    },
    {
      ...runsColumn,
      sortable: true,
      sortAccessor: (agent) => agent.runs,
    },
    {
      ...costColumn,
      sortable: true,
      sortAccessor: (agent) => agent.cost,
    },
    {
      id: 'lastRun',
      header: 'Last run',
      gridWidth: '110px',
      cell: (agent) => (
        <Text size="supporting" color="muted">
          {agent.lastRun}
        </Text>
      ),
    },
  ];

  return (
    <Stack gap="snug">
      <DataTable
        data={rows}
        columns={columns}
        getRowKey={getKey}
        noun="agents"
        searchPlaceholder="Search agents"
        searchFn={searchAgents}
        defaultSortId="runs"
        defaultSortDir="desc"
        quickFilters={
          <SegmentedControl
            aria-label="Show agents by status"
            value={quick}
            onChange={setQuick}
            options={[
              { value: 'all', label: 'All', count: agents.length },
              { value: 'running', label: 'Running', count: count('Running') },
              { value: 'failed', label: 'Failed', count: count('Failed') },
            ]}
          />
        }
        toolbarActions={<Button size="sm">New agent</Button>}
        onRowClick={(agent) => setOpened(agent.name)}
        rowLabel={(agent) => `Open ${agent.name}`}
        pagination={{ pageSizeOptions: [5, 10, 25], defaultPageSize: 10 }}
        emptyMessage="No agents yet."
        filteredEmptyMessage="No agents match your search."
      />
      <Text size="supporting" color="muted">
        {opened ? `Opened ${opened}.` : 'Click a row to open an agent.'}
      </Text>
    </Stack>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-4xl space-y-8">
      <Example title="Agents">
        <AgentsOverview />
      </Example>
    </div>
  ),
};

function ServerFilterTable() {
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const rows = selected.size === 0 ? sample : sample.filter((a) => selected.has(a.status));

  return (
    <DataTable
      data={rows}
      noun="agents"
      getRowKey={getKey}
      columns={[
        nameColumn,
        {
          ...statusColumn,
          filter: {
            mode: 'server',
            options: statusOptions,
            selected,
            onToggle: (value) => setSelected((prev) => toggleValue(prev, value)),
            onClear: () => setSelected(new Set()),
          },
        },
        runsColumn,
      ]}
    />
  );
}

function RowClickTable() {
  const [opened, setOpened] = useState<string | null>(null);

  return (
    <Stack gap="snug">
      <DataTable
        data={sample}
        noun="agents"
        getRowKey={getKey}
        columns={[nameColumn, statusColumn, runsColumn]}
        onRowClick={(agent) => setOpened(agent.name)}
        rowLabel={(agent) => `Open ${agent.name}`}
      />
      <Text size="supporting" color="muted">
        {opened ? `Opened ${opened}.` : 'No row opened yet.'}
      </Text>
    </Stack>
  );
}

function QuickFilterTable() {
  const [quick, setQuick] = useState('all');
  const rows = quick === 'all' ? sample : sample.filter((a) => a.status === 'Running');

  return (
    <DataTable
      data={rows}
      noun="agents"
      getRowKey={getKey}
      columns={[nameColumn, statusColumn, runsColumn]}
      quickFilters={
        <SegmentedControl
          aria-label="Show sample agents by status"
          value={quick}
          onChange={setQuick}
          options={[
            { value: 'all', label: 'All', count: sample.length },
            {
              value: 'running',
              label: 'Running',
              count: sample.filter((a) => a.status === 'Running').length,
            },
          ]}
        />
      }
    />
  );
}

function ErrorTable() {
  const [failed, setFailed] = useState(true);

  return (
    <DataTable
      data={failed ? [] : sample}
      noun="agents"
      getRowKey={getKey}
      columns={[nameColumn, statusColumn, runsColumn]}
      error={
        failed ? (
          <Stack align="center" gap="snug">
            <Text size="supporting" color="muted">
              Could not load agents.
            </Text>
            <Button variant="outline" size="sm" onClick={() => setFailed(false)}>
              Try again
            </Button>
          </Stack>
        ) : undefined
      }
    />
  );
}

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl space-y-10">
      <OptionTable
        title="Finding rows"
        note="Search, sort and filter work on the rows you pass in. Sorting and client filters run inside the table."
      >
        <Option
          name="searchFn"
          use="Adds a search box. Return true for rows that match the lowercase query."
        >
          <DataTable
            data={sample}
            noun="agents"
            getRowKey={getKey}
            columns={[nameColumn, statusColumn, runsColumn]}
            searchPlaceholder="Search agents"
            searchFn={searchAgents}
          />
        </Option>
        <Option
          name="sortable, sortAccessor, defaultSortId"
          use="A sortable column needs a sortAccessor. defaultSortId and defaultSortDir set the starting order."
        >
          <DataTable
            data={sample}
            noun="agents"
            getRowKey={getKey}
            columns={[
              nameColumn,
              statusColumn,
              {
                ...runsColumn,
                sortable: true,
                sortAccessor: (agent) => agent.runs,
              },
            ]}
            defaultSortId="runs"
            defaultSortDir="desc"
          />
        </Option>
        <Option
          name='filter: { mode: "client" }'
          use="The table keeps the selection and hides rows for you. Open the filter next to Status."
        >
          <DataTable
            data={sample}
            noun="agents"
            getRowKey={getKey}
            columns={[nameColumn, { ...statusColumn, filter: statusFilter }, runsColumn]}
          />
        </Option>
        <Option
          name='filter: { mode: "server" }'
          use="You keep the selection with selected and onToggle, then pass in the rows that match. Use it when the server does the filtering."
        >
          <ServerFilterTable />
        </Option>
      </OptionTable>

      <OptionTable title="Reading rows" note="Control what each row shows and what it does.">
        <Option
          name="defaultVisible, locked"
          use="defaultVisible: false hides a column until it is turned on in Columns. A locked column is always shown and is not in the menu."
        >
          <DataTable
            data={sample}
            noun="agents"
            getRowKey={getKey}
            columns={[nameColumn, statusColumn, { ...costColumn, defaultVisible: false }]}
          />
        </Option>
        <Option
          name='align="right" / "center"'
          use="Aligns the header and the cells. Right suits numbers."
        >
          <DataTable
            data={sample}
            noun="agents"
            getRowKey={getKey}
            columns={[
              nameColumn,
              { ...statusColumn, align: 'center' },
              { ...costColumn, align: 'right' },
            ]}
          />
        </Option>
        <Option
          name="onRowClick, rowLabel"
          use="Makes each row a button. rowLabel gives it a name for screen readers."
        >
          <RowClickTable />
        </Option>
      </OptionTable>

      <OptionTable
        title="Around the table"
        note="The toolbar, the footer and the messages shown when there is nothing to list."
      >
        <Option name="toolbarTitle" use="A title on the left of the toolbar.">
          <DataTable
            data={sample}
            noun="agents"
            getRowKey={getKey}
            columns={[nameColumn, statusColumn, runsColumn]}
            toolbarTitle={
              <Text size="supporting" weight="emphasis">
                Agents
              </Text>
            }
          />
        </Option>
        <Option
          name="toolbarActions"
          use="Buttons on the right of the toolbar, after the search box."
        >
          <DataTable
            data={sample}
            noun="agents"
            getRowKey={getKey}
            columns={[nameColumn, statusColumn, runsColumn]}
            toolbarActions={<Button size="sm">New agent</Button>}
          />
        </Option>
        <Option
          name="quickFilters"
          use="Any control in the toolbar, next to Columns. You filter the rows yourself."
        >
          <QuickFilterTable />
        </Option>
        <Option
          name="pagination, noun"
          use="Splits rows into pages with a page size menu. The footer shows only when there is more than one page."
        >
          <DataTable
            data={sample}
            noun="agents"
            getRowKey={getKey}
            columns={[nameColumn, statusColumn, runsColumn]}
            pagination={{ pageSizeOptions: [3, 6], defaultPageSize: 3 }}
          />
        </Option>
        <Option name="emptyMessage" use="Shown when there are no rows and nothing is filtered.">
          <DataTable
            data={[]}
            noun="agents"
            getRowKey={getKey}
            columns={[nameColumn, statusColumn, runsColumn]}
            emptyMessage="No agents yet."
          />
        </Option>
        <Option
          name="filteredEmptyMessage"
          use="Shown when a search or filter is active and no rows match. Here a Failed filter is set and no agent has failed."
        >
          <DataTable
            data={[]}
            noun="agents"
            getRowKey={getKey}
            columns={[
              nameColumn,
              {
                ...statusColumn,
                filter: {
                  mode: 'server',
                  options: statusOptions,
                  selected: new Set(['Failed']),
                  onToggle: () => {},
                },
              },
              runsColumn,
            ]}
            filteredEmptyMessage="No agents match your filters."
          />
        </Option>
        <Option name="error" use="Replaces the rows when loading failed. Add a way to try again.">
          <ErrorTable />
        </Option>
      </OptionTable>
    </div>
  ),
};
