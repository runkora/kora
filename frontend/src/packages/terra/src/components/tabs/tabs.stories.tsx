import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Tabs, TabsList, TabsTrigger, TabsContent } from './tabs';
import { Badge } from '../badge/badge';
import { Text } from '../typography/typography';
import { Stack } from '../layout/stack';

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
        <Text size="supporting" color="muted">Agent is running and processing tasks.</Text>
      </TabsContent>
      <TabsContent value="events">
        <Text size="supporting" color="muted">No recent events.</Text>
      </TabsContent>
      <TabsContent value="settings">
        <Text size="supporting" color="muted">Configure agent behaviour and limits.</Text>
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
          <Text size="micro" color="muted" weight="emphasis" transform="uppercase">
            Cost — last 7 days
          </Text>
          {[
            { day: 'Mon', amount: '$1.42' },
            { day: 'Tue', amount: '$2.18' },
            { day: 'Wed', amount: '$1.87' },
            { day: 'Thu', amount: '$3.05' },
          ].map(({ day, amount }) => (
            <Stack key={day} direction="horizontal" justify="between" align="center">
              <Text size="supporting" color="muted">{day}</Text>
              <Text size="supporting" mono tabular color="foreground">{amount}</Text>
            </Stack>
          ))}
        </div>
      </TabsContent>

      <TabsContent value="tokens">
        <div className="space-y-3">
          <Text size="micro" color="muted" weight="emphasis" transform="uppercase">Token usage</Text>
          {[
            { label: 'Prompt tokens', value: '1,241,883' },
            { label: 'Completion tokens', value: '489,201' },
            { label: 'Total tokens', value: '1,731,084' },
          ].map(({ label, value }) => (
            <Stack key={label} direction="horizontal" justify="between" align="center">
              <Text size="supporting" color="muted">{label}</Text>
              <Text size="supporting" mono tabular color="foreground">{value}</Text>
            </Stack>
          ))}
        </div>
      </TabsContent>

      <TabsContent value="policies">
        <div className="space-y-2">
          <Text size="micro" color="muted" weight="emphasis" transform="uppercase">Active policies</Text>
          {[
            { name: 'rate-limit-prod', decision: 'allow' },
            { name: 'block-pii-egress', decision: 'deny' },
            { name: 'audit-tool-calls', decision: 'audit' },
          ].map(({ name, decision }) => (
            <Stack key={name} direction="horizontal" justify="between" align="center">
              <Text size="micro" mono color="foreground">{name}</Text>
              <Badge
                variant={
                  decision === 'allow' ? 'healthy' : decision === 'deny' ? 'error' : 'degraded'
                }
                className="text-[10px]"
              >
                {decision}
              </Badge>
            </Stack>
          ))}
        </div>
      </TabsContent>

      <TabsContent value="credentials">
        <div className="space-y-2">
          <Text size="micro" color="muted" weight="emphasis" transform="uppercase">
            Brokered credentials
          </Text>
          {[
            { name: 'prod-s3-read', expires: '2d' },
            { name: 'gh-actions-token', expires: '12h' },
            { name: 'pg-readonly', expires: '30d' },
          ].map(({ name, expires }) => (
            <Stack key={name} direction="horizontal" justify="between" align="center">
              <Text size="micro" mono color="foreground">{name}</Text>
              <Text size="micro" tabular color="muted">expires in {expires}</Text>
            </Stack>
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
              <Text key={msg} size="micro" color="status-error">{msg}</Text>
            ),
          )}
        </div>
      </TabsContent>
      <TabsContent value="warnings">
        <Text size="supporting" color="muted">7 warnings across 3 agents.</Text>
      </TabsContent>
      <TabsContent value="info">
        <Text size="supporting" color="muted">No informational notices.</Text>
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
        <Text size="supporting" color="muted">Showing active agents.</Text>
      </TabsContent>
      <TabsContent value="archived">
        <Text size="supporting" color="muted">Showing archived agents.</Text>
      </TabsContent>
      <TabsContent value="beta">
        <Text size="supporting" color="muted">Beta features — not yet available.</Text>
      </TabsContent>
    </Tabs>
  ),
};

function ControlledDemo() {
  const [tab, setTab] = React.useState('keys');
  return (
    <div className="w-[440px] space-y-4">
      <div className="flex items-center justify-between">
        <Stack direction="horizontal" gap="tight" align="center">
          <Text size="micro" color="muted">Active:</Text>
          <Text size="micro" mono color="foreground">{tab}</Text>
        </Stack>
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
          <Text size="supporting" color="muted">Manage API keys for service-to-service auth.</Text>
        </TabsContent>
        <TabsContent value="tokens">
          <Text size="supporting" color="muted">Short-lived access tokens issued by the broker.</Text>
        </TabsContent>
      </Tabs>
    </div>
  );
}

export const Controlled: Story = {
  name: 'Controlled',
  render: () => <ControlledDemo />,
};
