'use client';

import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import {
  // Base primitives
  TableRoot,
  Table,
  TableHeader,
  TableBody,
  TableFooter,
  TableRow,
  TableHead,
  TableCell,
  TableCaption,
  // Extras
  TableContainer,
  TableEmpty,
  // Toolbar
  TableToolbar,
  TableToolbarShell,
  // Column header
  TableColumnHeader,
  // Utilities
  nextSortDir,
} from './index';
import type { SortDir, TableColumnVisibility, TableColumnFilter } from './index';

// ── Meta ──────────────────────────────────────────────────────────────────────

const meta: Meta = {
  title: 'Terra/Molecules/Table',
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj;

// ── Fixtures ──────────────────────────────────────────────────────────────────

const agents = [
  { id: 'apollo-3', status: 'Running', tools: 3, uptime: '4d 12h', cpu: '42%' },
  { id: 'hermes-1', status: 'Idle', tools: 0, uptime: '2d 3h', cpu: '1%' },
  { id: 'minerva-2', status: 'Suspended', tools: 1, uptime: '0d 8h', cpu: '0%' },
  { id: 'daedalus-4', status: 'Error', tools: 2, uptime: '1d 1h', cpu: '89%' },
  { id: 'icarus-5', status: 'Running', tools: 4, uptime: '6d 22h', cpu: '61%' },
];

const credentials = [
  { name: 'prod-s3-read', type: 'AWS IAM', scope: 's3:GetObject', expires: '2d' },
  { name: 'gh-actions-token', type: 'GitHub', scope: 'repo:read', expires: '12h' },
  { name: 'pg-readonly', type: 'Postgres', scope: 'SELECT', expires: '30d' },
  { name: 'vault-transit', type: 'Vault', scope: 'encrypt', expires: '6h' },
];

const resources = [
  { resource: 'CPU cores', allocated: '32', used: '18' },
  { resource: 'Memory (GiB)', allocated: '128', used: '74' },
  { resource: 'Disk (TiB)', allocated: '10', used: '3.2' },
  { resource: 'Network (Gbps)', allocated: '40', used: '12' },
];

// ── 1. Default — basic table ──────────────────────────────────────────────────

export const Default: Story = {
  name: 'Default',
  render: () => (
    <div className="w-[560px]">
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Agent ID</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Tools</TableHead>
              <TableHead>Uptime</TableHead>
              <TableHead>CPU</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {agents.map((agent) => (
              <TableRow key={agent.id}>
                <TableCell>
                  <code>{agent.id}</code>
                </TableCell>
                <TableCell>{agent.status}</TableCell>
                <TableCell>{agent.tools}</TableCell>
                <TableCell>{agent.uptime}</TableCell>
                <TableCell>{agent.cpu}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableRoot>
    </div>
  ),
};

// ── 2. With caption ───────────────────────────────────────────────────────────

export const WithCaption: Story = {
  name: 'With Caption',
  render: () => (
    <div className="w-[560px]">
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Agent ID</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Tools</TableHead>
              <TableHead>CPU</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {agents.map((agent) => (
              <TableRow key={agent.id}>
                <TableCell>
                  <code>{agent.id}</code>
                </TableCell>
                <TableCell>{agent.status}</TableCell>
                <TableCell>{agent.tools}</TableCell>
                <TableCell>{agent.cpu}</TableCell>
              </TableRow>
            ))}
          </TableBody>
          <TableCaption>5 agents · last refreshed just now</TableCaption>
        </Table>
      </TableRoot>
    </div>
  ),
};

// ── 3. With footer ────────────────────────────────────────────────────────────

export const WithFooter: Story = {
  name: 'With Footer',
  render: () => (
    <div className="w-[400px]">
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Resource</TableHead>
              <TableHead>Allocated</TableHead>
              <TableHead>Used</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {resources.map((row) => (
              <TableRow key={row.resource}>
                <TableCell>{row.resource}</TableCell>
                <TableCell>{row.allocated}</TableCell>
                <TableCell>{row.used}</TableCell>
              </TableRow>
            ))}
          </TableBody>
          <TableFooter>
            <TableRow>
              <TableCell>Total</TableCell>
              <TableCell>—</TableCell>
              <TableCell>—</TableCell>
            </TableRow>
          </TableFooter>
        </Table>
      </TableRoot>
    </div>
  ),
};

// ── 4. Credentials ────────────────────────────────────────────────────────────

export const Credentials: Story = {
  name: 'Credentials',
  render: () => (
    <div className="w-[540px]">
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Scope</TableHead>
              <TableHead>Expires</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {credentials.map((cred) => (
              <TableRow key={cred.name}>
                <TableCell>
                  <code>{cred.name}</code>
                </TableCell>
                <TableCell>{cred.type}</TableCell>
                <TableCell>
                  <code>{cred.scope}</code>
                </TableCell>
                <TableCell>{cred.expires}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableRoot>
    </div>
  ),
};

// ── 5. TableContainer — card shell ────────────────────────────────────────────

export const WithContainer: Story = {
  name: 'TableContainer (card shell)',
  render: () => (
    <div className="w-[560px]">
      <TableContainer>
        <TableRoot>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Agent ID</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Tools</TableHead>
                <TableHead>Uptime</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {agents.map((agent) => (
                <TableRow key={agent.id}>
                  <TableCell>
                    <code>{agent.id}</code>
                  </TableCell>
                  <TableCell>{agent.status}</TableCell>
                  <TableCell>{agent.tools}</TableCell>
                  <TableCell>{agent.uptime}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableRoot>
      </TableContainer>
    </div>
  ),
};

// ── 6. TableEmpty ─────────────────────────────────────────────────────────────

export const Empty: Story = {
  name: 'TableEmpty',
  render: () => (
    <div className="w-[480px]">
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Agent ID</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Tools</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableEmpty colSpan={3} message="No agents found." />
          </TableBody>
        </Table>
      </TableRoot>
    </div>
  ),
};

// ── 7. TableColumnHeader — sort only ─────────────────────────────────────────

export const ColumnHeaderSort: Story = {
  name: 'TableColumnHeader — Sort',
  render: () => {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const [sortCol, setSortCol] = useState<string>('name');
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const [sortDir, setSortDir] = useState<SortDir>('asc');

    function handleSort(col: string) {
      if (sortCol === col) {
        setSortDir((d) => nextSortDir(d));
      } else {
        setSortCol(col);
        setSortDir('asc');
      }
    }

    return (
      <div className="w-[480px]">
        <TableRoot>
          <Table>
            <TableHeader>
              <TableRow>
                <TableColumnHeader
                  sortDir={sortCol === 'name' ? sortDir : null}
                  onSort={() => handleSort('name')}
                >
                  Name
                </TableColumnHeader>
                <TableColumnHeader
                  sortDir={sortCol === 'status' ? sortDir : null}
                  onSort={() => handleSort('status')}
                >
                  Status
                </TableColumnHeader>
                <TableColumnHeader
                  align="right"
                  sortDir={sortCol === 'cpu' ? sortDir : null}
                  onSort={() => handleSort('cpu')}
                >
                  CPU
                </TableColumnHeader>
              </TableRow>
            </TableHeader>
            <TableBody>
              {agents.map((agent) => (
                <TableRow key={agent.id}>
                  <TableCell>
                    <code>{agent.id}</code>
                  </TableCell>
                  <TableCell>{agent.status}</TableCell>
                  <TableCell>{agent.cpu}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableRoot>
      </div>
    );
  },
};

// ── 8. TableColumnHeader — filter only ───────────────────────────────────────

const STATUS_OPTS: TableColumnFilter['options'] = [
  { value: 'Running', label: 'Running' },
  { value: 'Idle', label: 'Idle' },
  { value: 'Suspended', label: 'Suspended' },
  { value: 'Error', label: 'Error' },
];

export const ColumnHeaderFilter: Story = {
  name: 'TableColumnHeader — Filter',
  render: () => {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const [selected, setSelected] = useState<Set<string>>(new Set());

    function toggleStatus(v: string) {
      setSelected((prev) => {
        const next = new Set(prev);
        if (next.has(v)) next.delete(v);
        else next.add(v);
        return next;
      });
    }

    const filtered = selected.size > 0 ? agents.filter((a) => selected.has(a.status)) : agents;

    return (
      <div className="w-[480px]">
        <TableRoot>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Agent ID</TableHead>
                <TableColumnHeader
                  filter={{
                    options: STATUS_OPTS,
                    selected,
                    onToggle: toggleStatus,
                    onClear: () => setSelected(new Set()),
                  }}
                >
                  Status
                </TableColumnHeader>
                <TableHead>Tools</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.length === 0 ? (
                <TableEmpty colSpan={3} message="No agents match this filter." />
              ) : (
                filtered.map((agent) => (
                  <TableRow key={agent.id}>
                    <TableCell>
                      <code>{agent.id}</code>
                    </TableCell>
                    <TableCell>{agent.status}</TableCell>
                    <TableCell>{agent.tools}</TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableRoot>
      </div>
    );
  },
};

// ── 9. TableColumnHeader — sort + filter ─────────────────────────────────────

export const ColumnHeaderSortAndFilter: Story = {
  name: 'TableColumnHeader — Sort + Filter',
  render: () => {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const [sortDir, setSortDir] = useState<SortDir>('asc');
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const [statusFilter, setStatusFilter] = useState<Set<string>>(new Set());

    const sorted = [...agents].sort((a, b) =>
      sortDir === 'asc' ? a.id.localeCompare(b.id) : b.id.localeCompare(a.id),
    );
    const filtered =
      statusFilter.size > 0 ? sorted.filter((a) => statusFilter.has(a.status)) : sorted;

    function toggleStatus(v: string) {
      setStatusFilter((prev) => {
        const next = new Set(prev);
        if (next.has(v)) next.delete(v);
        else next.add(v);
        return next;
      });
    }

    return (
      <div className="w-[480px]">
        <TableRoot>
          <Table>
            <TableHeader>
              <TableRow>
                <TableColumnHeader
                  sortDir={sortDir}
                  onSort={() => setSortDir((d) => nextSortDir(d))}
                >
                  Agent ID
                </TableColumnHeader>
                <TableColumnHeader
                  filter={{
                    options: STATUS_OPTS,
                    selected: statusFilter,
                    onToggle: toggleStatus,
                    onClear: () => setStatusFilter(new Set()),
                  }}
                >
                  Status
                </TableColumnHeader>
                <TableHead>Tools</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.length === 0 ? (
                <TableEmpty colSpan={3} message="No agents match this filter." />
              ) : (
                filtered.map((agent) => (
                  <TableRow key={agent.id}>
                    <TableCell>
                      <code>{agent.id}</code>
                    </TableCell>
                    <TableCell>{agent.status}</TableCell>
                    <TableCell>{agent.tools}</TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableRoot>
      </div>
    );
  },
};

// ── 10. Toolbar stories ───────────────────────────────────────────────────────
//
// One story per prop group so each can be understood in isolation,
// then a combined story showing all three groups together.

const TOOLBAR_COLS: TableColumnVisibility[] = [
  { id: 'status', label: 'Status', visible: true },
  { id: 'tools', label: 'Tools', visible: true },
  { id: 'uptime', label: 'Uptime', visible: false },
  { id: 'cpu', label: 'CPU', visible: false },
];

// 10a — search group only
export const ToolbarSearch: Story = {
  name: 'TableToolbar — search',
  render: () => {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const [q, setQ] = useState('');
    return (
      <div className="w-[480px]">
        <TableContainer>
          <TableToolbar search={{ value: q, onChange: setQ, placeholder: 'Search agents…' }} />
        </TableContainer>
      </div>
    );
  },
};

// 10b — count group only
export const ToolbarCount: Story = {
  name: 'TableToolbar — count',
  render: () => (
    <div className="w-[480px]">
      <TableContainer>
        <TableToolbar count={{ showing: 3, total: 5, label: 'agents' }} />
      </TableContainer>
    </div>
  ),
};

// 10c — columns group only
export const ToolbarColumns: Story = {
  name: 'TableToolbar — columns toggle',
  render: () => {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const [cols, setCols] = useState<TableColumnVisibility[]>(TOOLBAR_COLS);
    function toggle(id: string) {
      setCols((prev) => prev.map((c) => (c.id === id ? { ...c, visible: !c.visible } : c)));
    }
    return (
      <div className="w-[480px]">
        <TableContainer>
          <TableToolbar columns={{ items: cols, onToggle: toggle }} />
        </TableContainer>
      </div>
    );
  },
};

// 10d — all three groups together (isolated toolbar demo, no table rows)
export const Toolbar: Story = {
  name: 'TableToolbar — all groups',
  render: () => {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const [q, setQ] = useState('');
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const [cols, setCols] = useState<TableColumnVisibility[]>(TOOLBAR_COLS);

    function toggle(id: string) {
      setCols((prev) => prev.map((c) => (c.id === id ? { ...c, visible: !c.visible } : c)));
    }

    const showing = agents.filter((a) => !q || a.id.toLowerCase().includes(q.toLowerCase())).length;

    return (
      <div className="w-[640px]">
        <TableContainer>
          <TableToolbar
            search={{ value: q, onChange: setQ, placeholder: 'Search agents…' }}
            count={{ showing, total: agents.length, label: 'agents' }}
            columns={{ items: cols, onToggle: toggle }}
          />
        </TableContainer>
      </div>
    );
  },
};

// ── TableToolbarShell ─────────────────────────────────────────────────────────

import { Skeleton } from '../skeleton';

/** TableToolbarShell — bare container for skeleton placeholders and custom toolbar content */
export const ToolbarShell: StoryObj = {
  name: 'TableToolbarShell — skeleton',
  render: () => (
    <TableToolbarShell>
      <Skeleton shape="block" width="quarter" size="sm" />
      <div style={{ marginLeft: 'auto', display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
        <Skeleton shape="line" width="quarter" size="xs" />
        <Skeleton shape="block" width="quarter" size="sm" />
      </div>
    </TableToolbarShell>
  ),
};
