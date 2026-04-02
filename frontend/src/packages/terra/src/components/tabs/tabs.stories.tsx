import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Tabs, TabsList, TabsTrigger, TabsContent } from './tabs';
import { Badge } from '../badge/badge';

const meta: Meta = {
  title: 'Terra/Molecules/Tabs',
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj;

export const Default: Story = {
  render: () => (
    <Tabs defaultValue="overview" className="w-[480px]">
      <TabsList>
        <TabsTrigger value="overview">Overview</TabsTrigger>
        <TabsTrigger value="events">Events</TabsTrigger>
        <TabsTrigger value="settings">Settings</TabsTrigger>
      </TabsList>
      <TabsContent value="overview">
        <p className="text-sm text-muted">Agent is running and processing tasks.</p>
      </TabsContent>
      <TabsContent value="events">
        <p className="text-sm text-muted">No recent events.</p>
      </TabsContent>
      <TabsContent value="settings">
        <p className="text-sm text-muted">Configure agent behaviour and limits.</p>
      </TabsContent>
    </Tabs>
  ),
};

export const AgentDetail: Story = {
  name: 'Agent detail panel',
  render: () => (
    <Tabs defaultValue="spend" className="w-[520px]">
      <TabsList>
        <TabsTrigger value="spend">Spend</TabsTrigger>
        <TabsTrigger value="tokens">Tokens</TabsTrigger>
        <TabsTrigger value="policies">Policies</TabsTrigger>
        <TabsTrigger value="credentials">Credentials</TabsTrigger>
      </TabsList>

      <TabsContent value="spend">
        <div className="space-y-3">
          <p className="text-xs font-medium uppercase tracking-wider text-muted">
            Cost — last 7 days
          </p>
          {[
            { day: 'Mon', amount: '$1.42' },
            { day: 'Tue', amount: '$2.18' },
            { day: 'Wed', amount: '$1.87' },
            { day: 'Thu', amount: '$3.05' },
          ].map(({ day, amount }) => (
            <div key={day} className="flex items-center justify-between text-sm">
              <span className="text-muted">{day}</span>
              <span className="font-mono tabular-nums text-foreground">{amount}</span>
            </div>
          ))}
        </div>
      </TabsContent>

      <TabsContent value="tokens">
        <div className="space-y-3">
          <p className="text-xs font-medium uppercase tracking-wider text-muted">Token usage</p>
          {[
            { label: 'Prompt tokens', value: '1,241,883' },
            { label: 'Completion tokens', value: '489,201' },
            { label: 'Total tokens', value: '1,731,084' },
          ].map(({ label, value }) => (
            <div key={label} className="flex items-center justify-between text-sm">
              <span className="text-muted">{label}</span>
              <span className="font-mono tabular-nums text-foreground">{value}</span>
            </div>
          ))}
        </div>
      </TabsContent>

      <TabsContent value="policies">
        <div className="space-y-2">
          <p className="text-xs font-medium uppercase tracking-wider text-muted">Active policies</p>
          {[
            { name: 'rate-limit-prod', decision: 'allow' },
            { name: 'block-pii-egress', decision: 'deny' },
            { name: 'audit-tool-calls', decision: 'audit' },
          ].map(({ name, decision }) => (
            <div key={name} className="flex items-center justify-between">
              <span className="font-mono text-xs text-foreground">{name}</span>
              <Badge
                variant={
                  decision === 'allow' ? 'healthy' : decision === 'deny' ? 'error' : 'degraded'
                }
                className="text-[10px]"
              >
                {decision}
              </Badge>
            </div>
          ))}
        </div>
      </TabsContent>

      <TabsContent value="credentials">
        <div className="space-y-2">
          <p className="text-xs font-medium uppercase tracking-wider text-muted">
            Brokered credentials
          </p>
          {[
            { name: 'prod-s3-read', expires: '2d' },
            { name: 'gh-actions-token', expires: '12h' },
            { name: 'pg-readonly', expires: '30d' },
          ].map(({ name, expires }) => (
            <div key={name} className="flex items-center justify-between text-xs">
              <span className="font-mono text-foreground">{name}</span>
              <span className="tabular-nums text-muted">expires in {expires}</span>
            </div>
          ))}
        </div>
      </TabsContent>
    </Tabs>
  ),
};

export const WithBadgeCounts: Story = {
  name: 'With badge counts',
  render: () => (
    <Tabs defaultValue="alerts" className="w-[440px]">
      <TabsList>
        <TabsTrigger value="alerts">
          Alerts
          <Badge variant="error" className="ml-1.5 text-[10px] px-1.5 py-0">
            3
          </Badge>
        </TabsTrigger>
        <TabsTrigger value="warnings">
          Warnings
          <Badge variant="degraded" className="ml-1.5 text-[10px] px-1.5 py-0">
            7
          </Badge>
        </TabsTrigger>
        <TabsTrigger value="info">Info</TabsTrigger>
      </TabsList>
      <TabsContent value="alerts">
        <div className="space-y-2">
          {['Rate limit exceeded — hermes-1', 'Auth failure — daedalus-4', 'OOM — icarus-5'].map(
            (msg) => (
              <p key={msg} className="text-xs text-status-error">
                {msg}
              </p>
            ),
          )}
        </div>
      </TabsContent>
      <TabsContent value="warnings">
        <p className="text-sm text-muted">7 warnings across 3 agents.</p>
      </TabsContent>
      <TabsContent value="info">
        <p className="text-sm text-muted">No informational notices.</p>
      </TabsContent>
    </Tabs>
  ),
};

export const WithDisabled: Story = {
  name: 'With disabled tab',
  render: () => (
    <Tabs defaultValue="active" className="w-[400px]">
      <TabsList>
        <TabsTrigger value="active">Active</TabsTrigger>
        <TabsTrigger value="archived">Archived</TabsTrigger>
        <TabsTrigger value="beta" disabled>
          Beta
        </TabsTrigger>
      </TabsList>
      <TabsContent value="active">
        <p className="text-sm text-muted">Showing active agents.</p>
      </TabsContent>
      <TabsContent value="archived">
        <p className="text-sm text-muted">Showing archived agents.</p>
      </TabsContent>
      <TabsContent value="beta">
        <p className="text-sm text-muted">Beta features — not yet available.</p>
      </TabsContent>
    </Tabs>
  ),
};

function ControlledDemo() {
  const [tab, setTab] = React.useState('keys');
  return (
    <div className="w-[440px] space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-xs text-muted">
          Active: <span className="font-mono text-foreground">{tab}</span>
        </p>
        <button
          className="text-xs text-accent hover:underline"
          onClick={() => setTab(tab === 'keys' ? 'tokens' : 'keys')}
        >
          toggle
        </button>
      </div>
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="keys">API keys</TabsTrigger>
          <TabsTrigger value="tokens">Access tokens</TabsTrigger>
        </TabsList>
        <TabsContent value="keys">
          <p className="text-sm text-muted">Manage API keys for service-to-service auth.</p>
        </TabsContent>
        <TabsContent value="tokens">
          <p className="text-sm text-muted">Short-lived access tokens issued by the broker.</p>
        </TabsContent>
      </Tabs>
    </div>
  );
}

export const Controlled: Story = {
  name: 'Controlled',
  render: () => <ControlledDemo />,
};
