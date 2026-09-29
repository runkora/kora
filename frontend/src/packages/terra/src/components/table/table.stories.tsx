'use client';

import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Badge } from '../badge';
import { Button } from '../button';
import { Skeleton } from '../skeleton';
import { Code } from '../typography';
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableColumnHeader,
  TableContainer,
  TableEmpty,
  TableFooter,
  TableHead,
  TableHeader,
  TableRoot,
  TableRow,
  TableToolbar,
  TableToolbarShell,
  nextSortDir,
} from './index';
import type { SortDir, TableColumnFilter, TableColumnVisibility } from './index';

/**
 * Table
 *
 * Shows rows of data in columns. Build it from a header and a body, and add a toolbar and sortable
 * column headers when people need to search, sort or filter.
 */
const meta: Meta<typeof Table> = {
  title: 'Terra/Molecules/Table',
  component: Table,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Table>;

type Status = 'Running' | 'Idle' | 'Suspended' | 'Error';

const agents: { name: string; status: Status; runs: number; cost: string }[] = [
  { name: 'apollo-3', status: 'Running', runs: 412, cost: '$4.12' },
  { name: 'hermes-1', status: 'Idle', runs: 87, cost: '$1.87' },
  { name: 'minerva-2', status: 'Suspended', runs: 254, cost: '$2.54' },
  { name: 'daedalus-4', status: 'Error', runs: 63, cost: '$0.63' },
  { name: 'icarus-5', status: 'Running', runs: 329, cost: '$3.29' },
];

const statusOptions: TableColumnFilter['options'] = [
  { value: 'Running', label: 'Running', dot: 'bg-status-healthy' },
  { value: 'Idle', label: 'Idle', dot: 'bg-status-idle' },
  { value: 'Suspended', label: 'Suspended', dot: 'bg-status-pending' },
  { value: 'Error', label: 'Error', dot: 'bg-status-error' },
];

const statusVariant = {
  Running: 'healthy',
  Idle: 'idle',
  Suspended: 'pending',
  Error: 'error',
} as const;

function StatusBadge({ status }: { status: Status }) {
  return <Badge variant={statusVariant[status]}>{status}</Badge>;
}

/** A small static table of agents, with optional extra parts inside the table element. */
function AgentTable({
  rows = agents,
  children,
}: {
  rows?: typeof agents;
  children?: React.ReactNode;
}) {
  return (
    <TableRoot>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Agent</TableHead>
            <TableHead>Status</TableHead>
            <TableHead align="right">Runs</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((agent) => (
            <TableRow key={agent.name}>
              <TableCell>
                <Code>{agent.name}</Code>
              </TableCell>
              <TableCell>
                <StatusBadge status={agent.status} />
              </TableCell>
              <TableCell className="text-right">{agent.runs}</TableCell>
            </TableRow>
          ))}
        </TableBody>
        {children}
      </Table>
    </TableRoot>
  );
}

function toggleValue(set: Set<string>, value: string) {
  const next = new Set(set);
  if (next.has(value)) next.delete(value);
  else next.add(value);
  return next;
}

function AgentsOverview() {
  const [query, setQuery] = useState('');
  const [sortDir, setSortDir] = useState<SortDir>('desc');
  const [statuses, setStatuses] = useState<Set<string>>(new Set());
  const [columns, setColumns] = useState<TableColumnVisibility[]>([
    { id: 'status', label: 'Status', visible: true },
    { id: 'runs', label: 'Runs', visible: true },
    { id: 'cost', label: 'Cost', visible: true },
  ]);

  const shown = (id: string) => columns.find((c) => c.id === id)?.visible;
  const rows = agents
    .filter((a) => a.name.includes(query.toLowerCase()))
    .filter((a) => statuses.size === 0 || statuses.has(a.status))
    .sort((a, b) => (sortDir === 'asc' ? a.runs - b.runs : b.runs - a.runs));
  const visibleCount = 1 + columns.filter((c) => c.visible).length;

  return (
    <TableContainer>
      <TableToolbar
        search={{ value: query, onChange: setQuery, placeholder: 'Search agents' }}
        count={{ showing: rows.length, total: agents.length, label: 'agents' }}
        columns={{
          items: columns,
          onToggle: (id) =>
            setColumns((prev) =>
              prev.map((c) => (c.id === id ? { ...c, visible: !c.visible } : c)),
            ),
        }}
      />
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Agent</TableHead>
              {shown('status') && (
                <TableColumnHeader
                  filter={{
                    options: statusOptions,
                    selected: statuses,
                    onToggle: (v) => setStatuses((prev) => toggleValue(prev, v)),
                    onClear: () => setStatuses(new Set()),
                  }}
                >
                  Status
                </TableColumnHeader>
              )}
              {shown('runs') && (
                <TableColumnHeader
                  align="right"
                  sortDir={sortDir}
                  onSort={() => setSortDir((d) => nextSortDir(d) ?? 'asc')}
                >
                  Runs
                </TableColumnHeader>
              )}
              {shown('cost') && <TableHead align="right">Cost</TableHead>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableEmpty colSpan={visibleCount} message="No agents match." />
            ) : (
              rows.map((agent) => (
                <TableRow key={agent.name}>
                  <TableCell>
                    <Code>{agent.name}</Code>
                  </TableCell>
                  {shown('status') && (
                    <TableCell>
                      <StatusBadge status={agent.status} />
                    </TableCell>
                  )}
                  {shown('runs') && <TableCell className="text-right">{agent.runs}</TableCell>}
                  {shown('cost') && <TableCell className="text-right">{agent.cost}</TableCell>}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </TableRoot>
    </TableContainer>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-3xl space-y-8">
      <Example title="Search, filter, sort and hide columns">
        <AgentsOverview />
      </Example>
      <Example title="A plain table">
        <TableContainer>
          <AgentTable />
        </TableContainer>
      </Example>
    </div>
  ),
};

function SortDemo() {
  const [dir, setDir] = useState<SortDir>('asc');
  const rows = [...agents].sort((a, b) =>
    dir === 'desc' ? b.name.localeCompare(a.name) : a.name.localeCompare(b.name),
  );
  return (
    <TableContainer>
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableColumnHeader sortDir={dir} onSort={() => setDir((d) => nextSortDir(d))}>
                Agent
              </TableColumnHeader>
              <TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.slice(0, 3).map((agent) => (
              <TableRow key={agent.name}>
                <TableCell>
                  <Code>{agent.name}</Code>
                </TableCell>
                <TableCell>
                  <StatusBadge status={agent.status} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableRoot>
    </TableContainer>
  );
}

function FilterDemo({ withSort = false }: { withSort?: boolean }) {
  const [selected, setSelected] = useState<Set<string>>(new Set(['Running']));
  const [dir, setDir] = useState<SortDir>('asc');
  const rows = agents
    .filter((a) => selected.size === 0 || selected.has(a.status))
    .sort((a, b) => (dir === 'desc' ? b.runs - a.runs : a.runs - b.runs));
  return (
    <TableContainer>
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Agent</TableHead>
              <TableColumnHeader
                filter={{
                  options: statusOptions,
                  selected,
                  onToggle: (v) => setSelected((prev) => toggleValue(prev, v)),
                  onClear: () => setSelected(new Set()),
                }}
              >
                Status
              </TableColumnHeader>
              {withSort ? (
                <TableColumnHeader
                  align="right"
                  sortDir={dir}
                  onSort={() => setDir((d) => nextSortDir(d))}
                >
                  Runs
                </TableColumnHeader>
              ) : (
                <TableHead align="right">Runs</TableHead>
              )}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableEmpty colSpan={3} message="No agents match." />
            ) : (
              rows.map((agent) => (
                <TableRow key={agent.name}>
                  <TableCell>
                    <Code>{agent.name}</Code>
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={agent.status} />
                  </TableCell>
                  <TableCell className="text-right">{agent.runs}</TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </TableRoot>
    </TableContainer>
  );
}

function SearchDemo() {
  const [query, setQuery] = useState('');
  return (
    <TableContainer>
      <TableToolbar search={{ value: query, onChange: setQuery, placeholder: 'Search agents' }} />
    </TableContainer>
  );
}

function ColumnsDemo() {
  const [columns, setColumns] = useState<TableColumnVisibility[]>([
    { id: 'status', label: 'Status', visible: true },
    { id: 'runs', label: 'Runs', visible: true },
    { id: 'cost', label: 'Cost', visible: false },
  ]);
  return (
    <TableContainer>
      <TableToolbar
        columns={{
          items: columns,
          onToggle: (id) =>
            setColumns((prev) =>
              prev.map((c) => (c.id === id ? { ...c, visible: !c.visible } : c)),
            ),
        }}
      />
    </TableContainer>
  );
}

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl space-y-10">
      <OptionTable
        title="Table parts"
        note="TableRoot scrolls a wide table sideways. TableContainer adds the rounded border."
      >
        <Option name="TableContainer" use="A rounded border around a toolbar and a table.">
          <TableContainer>
            <AgentTable rows={agents.slice(0, 2)} />
          </TableContainer>
        </Option>
        <Option
          name='TableHead align="right"'
          use="Line a header up with a column of numbers. Also accepts center."
        >
          <AgentTable rows={agents.slice(0, 2)} />
        </Option>
        <Option
          name="TableHead srOnly"
          use="Hide a header from view but keep it for screen readers, such as an actions column."
        >
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Agent</TableHead>
                  <TableHead srOnly>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {agents.slice(0, 2).map((agent) => (
                  <TableRow key={agent.name}>
                    <TableCell>
                      <Code>{agent.name}</Code>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button size="sm" variant="ghost">
                        Open
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableRoot>
        </Option>
        <Option name='TableRow data-selected="true"' use="Tint a row to show it is selected.">
          <TableRoot>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Agent</TableHead>
                  <TableHead>Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {agents.slice(0, 3).map((agent, i) => (
                  <TableRow key={agent.name} data-selected={i === 1}>
                    <TableCell>
                      <Code>{agent.name}</Code>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={agent.status} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableRoot>
        </Option>
        <Option name="TableCaption" use="A short note under the table, such as a row count.">
          <AgentTable rows={agents.slice(0, 2)}>
            <TableCaption>5 agents, refreshed just now</TableCaption>
          </AgentTable>
        </Option>
        <Option name="TableFooter" use="A closing row for totals.">
          <AgentTable rows={agents.slice(0, 2)}>
            <TableFooter>
              <TableRow>
                <TableCell>Total</TableCell>
                <TableCell />
                <TableCell className="text-right">499</TableCell>
              </TableRow>
            </TableFooter>
          </AgentTable>
        </Option>
        <Option name="TableEmpty" use="A single row that says there is nothing to show.">
          <AgentTable rows={[]}>
            <TableBody>
              <TableEmpty colSpan={3} message="No agents yet." />
            </TableBody>
          </AgentTable>
        </Option>
      </OptionTable>

      <OptionTable
        title="Column headers"
        note="TableColumnHeader replaces TableHead when a column can be sorted or filtered. The click cycle is ascending, descending, off."
      >
        <Option name="sortDir, onSort" use="Add a sort button to the header.">
          <SortDemo />
        </Option>
        <Option
          name="filter"
          use="Add a filter menu. The icon turns to the accent colour while a filter is on."
        >
          <FilterDemo />
        </Option>
        <Option name="sortDir, onSort, filter" use="Sort and filter can sit on different columns.">
          <FilterDemo withSort />
        </Option>
      </OptionTable>

      <OptionTable
        title="Toolbar"
        note="Each group is optional. Leave one out to hide that part of the toolbar."
      >
        <Option name="search" use="A search box. You keep the text in your own state.">
          <SearchDemo />
        </Option>
        <Option name="count" use='Shows "Showing 3 of 5 agents". Set label to the row name.'>
          <TableContainer>
            <TableToolbar count={{ showing: 3, total: 5, label: 'agents' }} />
          </TableContainer>
        </Option>
        <Option name="columns" use="A Columns button that lets people show and hide columns.">
          <ColumnsDemo />
        </Option>
        <Option name="actions" use="Any extra controls, placed after the search box.">
          <TableContainer>
            <TableToolbar actions={<Button size="sm">New agent</Button>} />
          </TableContainer>
        </Option>
        <Option
          name="TableToolbarShell"
          use="The bare toolbar strip. Use it for loading placeholders or custom content."
        >
          <TableContainer>
            <TableToolbarShell>
              <Skeleton shape="block" width="quarter" size="sm" />
              <div className="ml-auto flex items-center gap-3">
                <Skeleton shape="line" width="quarter" size="xs" />
                <Skeleton shape="block" width="quarter" size="sm" />
              </div>
            </TableToolbarShell>
          </TableContainer>
        </Option>
      </OptionTable>
    </div>
  ),
};
