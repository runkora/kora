import type { Meta, StoryObj } from '@storybook/react';
import {
  TableRoot,
  Table,
  TableHeader,
  TableBody,
  TableFooter,
  TableRow,
  TableHead,
  TableCell,
  TableCaption,
} from './table';

const meta: Meta<typeof Table> = {
  title: 'Terra/Molecules/Table',
  component: Table,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Table>;

const agents = [
  { id: 'apollo-3', status: 'Running', tools: 3, uptime: '4d 12h', cpu: '42%' },
  { id: 'hermes-1', status: 'Idle', tools: 0, uptime: '2d 3h', cpu: '1%' },
  { id: 'minerva-2', status: 'Suspended', tools: 1, uptime: '0d 8h', cpu: '0%' },
  { id: 'daedalus-4', status: 'Error', tools: 2, uptime: '1d 1h', cpu: '89%' },
  { id: 'icarus-5', status: 'Running', tools: 4, uptime: '6d 22h', cpu: '61%' },
];

const statusColor: Record<string, string> = {
  Running: 'text-status-healthy',
  Idle: 'text-muted',
  Suspended: 'text-status-degraded',
  Error: 'text-status-error',
};

export const Default: Story = {
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
              <TableHead className="text-right">CPU</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {agents.map((agent) => (
              <TableRow key={agent.id}>
                <TableCell className="font-mono">{agent.id}</TableCell>
                <TableCell className={statusColor[agent.status]}>{agent.status}</TableCell>
                <TableCell>{agent.tools}</TableCell>
                <TableCell className="tabular-nums">{agent.uptime}</TableCell>
                <TableCell className="text-right tabular-nums">{agent.cpu}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableRoot>
    </div>
  ),
};

export const WithCaption: Story = {
  render: () => (
    <div className="w-[560px]">
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Agent ID</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Tools</TableHead>
              <TableHead className="text-right">CPU</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {agents.map((agent) => (
              <TableRow key={agent.id}>
                <TableCell className="font-mono">{agent.id}</TableCell>
                <TableCell className={statusColor[agent.status]}>{agent.status}</TableCell>
                <TableCell>{agent.tools}</TableCell>
                <TableCell className="text-right tabular-nums">{agent.cpu}</TableCell>
              </TableRow>
            ))}
          </TableBody>
          <TableCaption>5 agents · last refreshed just now</TableCaption>
        </Table>
      </TableRoot>
    </div>
  ),
};

export const WithFooter: Story = {
  render: () => (
    <div className="w-[400px]">
      <TableRoot>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Resource</TableHead>
              <TableHead className="text-right">Allocated</TableHead>
              <TableHead className="text-right">Used</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[
              { resource: 'CPU cores', allocated: '32', used: '18' },
              { resource: 'Memory (GiB)', allocated: '128', used: '74' },
              { resource: 'Disk (TiB)', allocated: '10', used: '3.2' },
              { resource: 'Network (Gbps)', allocated: '40', used: '12' },
            ].map((row) => (
              <TableRow key={row.resource}>
                <TableCell>{row.resource}</TableCell>
                <TableCell className="text-right tabular-nums">{row.allocated}</TableCell>
                <TableCell className="text-right tabular-nums">{row.used}</TableCell>
              </TableRow>
            ))}
          </TableBody>
          <TableFooter>
            <TableRow>
              <TableCell>Total</TableCell>
              <TableCell className="text-right tabular-nums">—</TableCell>
              <TableCell className="text-right tabular-nums">—</TableCell>
            </TableRow>
          </TableFooter>
        </Table>
      </TableRoot>
    </div>
  ),
};

export const Credentials: Story = {
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
            {[
              { name: 'prod-s3-read', type: 'AWS IAM', scope: 's3:GetObject', expires: '2d' },
              { name: 'gh-actions-token', type: 'GitHub', scope: 'repo:read', expires: '12h' },
              { name: 'pg-readonly', type: 'Postgres', scope: 'SELECT', expires: '30d' },
              { name: 'vault-transit', type: 'Vault', scope: 'encrypt', expires: '6h' },
            ].map((cred) => (
              <TableRow key={cred.name}>
                <TableCell className="font-mono text-xs">{cred.name}</TableCell>
                <TableCell className="text-muted">{cred.type}</TableCell>
                <TableCell className="font-mono text-xs text-accent">{cred.scope}</TableCell>
                <TableCell className="tabular-nums text-muted">{cred.expires}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableRoot>
    </div>
  ),
};

export const Empty: Story = {
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
            <TableRow>
              <TableCell colSpan={3} className="text-center text-muted py-10">
                No agents found.
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </TableRoot>
    </div>
  ),
};
