import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Badge } from '../badge/badge';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Tabs, TabsContent, TabsList, TabsTrigger } from './tabs';

/**
 * Tabs
 *
 * Switches between views of the same page, one view at a time. Use it when
 * each view is a different kind of content, not a filter on one list.
 */
const meta: Meta<typeof Tabs> = {
  title: 'Hades/Molecules/Tabs',
  component: Tabs,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Tabs>;

const COSTS = [
  { day: 'Mon', amount: '$1.42' },
  { day: 'Tue', amount: '$2.18' },
  { day: 'Wed', amount: '$1.87' },
];

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Agent page">
        <Tabs defaultValue="spend">
          <TabsList aria-label="Agent details">
            <TabsTrigger value="spend">Spend</TabsTrigger>
            <TabsTrigger value="tokens">Tokens</TabsTrigger>
            <TabsTrigger value="policies">Policies</TabsTrigger>
          </TabsList>
          <TabsContent value="spend">
            <Stack gap="snug">
              {COSTS.map(({ day, amount }) => (
                <Stack key={day} direction="horizontal" justify="between">
                  <Text size="supporting" color="muted">
                    {day}
                  </Text>
                  <Text size="supporting" mono tabular>
                    {amount}
                  </Text>
                </Stack>
              ))}
            </Stack>
          </TabsContent>
          <TabsContent value="tokens">
            <Text size="supporting" color="muted">
              48,200 input tokens and 9,600 output tokens in the last 7 days.
            </Text>
          </TabsContent>
          <TabsContent value="policies">
            <Text size="supporting" color="muted">
              3 policies apply to this agent.
            </Text>
          </TabsContent>
        </Tabs>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Every sample is live. Click a tab, or use the arrow keys, Home and End."
      >
        <Option
          name="Tabs defaultValue"
          use="The tab that is open first. Tabs manage their own state."
        >
          <Tabs defaultValue="events">
            <TabsList aria-label="Run details">
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="events">Events</TabsTrigger>
            </TabsList>
            <TabsContent value="overview">
              <Text size="supporting" color="muted">
                The run finished in 42 seconds.
              </Text>
            </TabsContent>
            <TabsContent value="events">
              <Text size="supporting" color="muted">
                12 events recorded.
              </Text>
            </TabsContent>
          </Tabs>
        </Option>
        <Option name='size="compact"' use="A tighter rail for side panels and inspectors.">
          <Tabs defaultValue="input" size="compact">
            <TabsList aria-label="Request details">
              <TabsTrigger value="input">Input</TabsTrigger>
              <TabsTrigger value="output">Output</TabsTrigger>
            </TabsList>
            <TabsContent value="input">
              <Text size="supporting" color="muted">
                Summarise the open pull requests.
              </Text>
            </TabsContent>
            <TabsContent value="output">
              <Text size="supporting" color="muted">
                Five pull requests are waiting for review.
              </Text>
            </TabsContent>
          </Tabs>
        </Option>
        <Option name="TabsTrigger icon" use="Put a small icon before the label.">
          <Tabs defaultValue="settings">
            <TabsList aria-label="Agent sections">
              <TabsTrigger value="activity" icon="activity">
                Activity
              </TabsTrigger>
              <TabsTrigger value="settings" icon="settings">
                Settings
              </TabsTrigger>
            </TabsList>
            <TabsContent value="activity">
              <Text size="supporting" color="muted">
                No activity today.
              </Text>
            </TabsContent>
            <TabsContent value="settings">
              <Text size="supporting" color="muted">
                Change the agent name and model.
              </Text>
            </TabsContent>
          </Tabs>
        </Option>
        <Option name="Badge inside a trigger" use="Show a count next to the label.">
          <Tabs defaultValue="alerts">
            <TabsList aria-label="Inbox">
              <TabsTrigger value="alerts">
                Alerts
                <Badge variant="error" size="sm">
                  3
                </Badge>
              </TabsTrigger>
              <TabsTrigger value="history">History</TabsTrigger>
            </TabsList>
            <TabsContent value="alerts">
              <Text size="supporting" color="muted">
                3 runs failed today.
              </Text>
            </TabsContent>
            <TabsContent value="history">
              <Text size="supporting" color="muted">
                Nothing older than 30 days.
              </Text>
            </TabsContent>
          </Tabs>
        </Option>
        <Option name="TabsTrigger disabled" use="Show a tab that cannot be opened yet.">
          <Tabs defaultValue="setup">
            <TabsList aria-label="Connector">
              <TabsTrigger value="setup">Setup</TabsTrigger>
              <TabsTrigger value="logs" disabled>
                Logs
              </TabsTrigger>
            </TabsList>
            <TabsContent value="setup">
              <Text size="supporting" color="muted">
                Add credentials to turn on logs.
              </Text>
            </TabsContent>
          </Tabs>
        </Option>
        <Option name="value and onValueChange" use="Control the open tab from your own state.">
          <ControlledTabs />
        </Option>
      </OptionTable>
    </div>
  ),
};

function ControlledTabs() {
  const [value, setValue] = React.useState('one');
  return (
    <Stack gap="snug">
      <Tabs value={value} onValueChange={setValue}>
        <TabsList aria-label="Steps">
          <TabsTrigger value="one">Step one</TabsTrigger>
          <TabsTrigger value="two">Step two</TabsTrigger>
        </TabsList>
        <TabsContent value="one">
          <Text size="supporting" color="muted">
            Choose a model.
          </Text>
        </TabsContent>
        <TabsContent value="two">
          <Text size="supporting" color="muted">
            Set a budget.
          </Text>
        </TabsContent>
      </Tabs>
      <Text size="caption" color="subtle" mono>
        value = {value}
      </Text>
    </Stack>
  );
}
