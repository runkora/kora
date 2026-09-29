import type { Meta, StoryObj } from '@storybook/react';
import { List, Stack } from '@orbit/terra';
import { Example, Option, OptionTable } from '../packages/terra/src/docs/story-ui';
import { EventFeedItem } from './event-feed-item';
import type { OrbitStatus } from './status-badge';

/**
 * EventFeedItem
 *
 * One row in an activity feed: a status dot, what happened, a short message and the time. Stack
 * several inside a List to show recent events.
 */
const meta: Meta<typeof EventFeedItem> = {
  title: 'App/Molecules/EventFeedItem',
  component: EventFeedItem,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof EventFeedItem>;

const STATUS_SAMPLES: { status: OrbitStatus; label: string; message: string }[] = [
  { status: 'healthy', label: 'policy.allow', message: 'Tool call approved within scope.' },
  { status: 'degraded', label: 'agent.slow', message: 'Response time passed the p99 limit.' },
  { status: 'error', label: 'policy.deny', message: 'Tool call blocked, outside allowed scope.' },
  { status: 'idle', label: 'agent.started', message: 'Ready and waiting for the first task.' },
  { status: 'pending', label: 'run.queued', message: 'Waiting for a free runner.' },
  { status: 'unknown', label: 'agent.seen', message: 'No status reported yet.' },
];

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Recent events">
        <List separator>
          <EventFeedItem
            status="healthy"
            label="policy.allow"
            meta="apollo-retriever"
            message="Tool call approved within scope."
            timestamp="09:41:12"
          />
          <EventFeedItem
            status="error"
            label="policy.deny"
            meta="daedalus-planner"
            message="Tool call blocked, outside allowed scope."
            timestamp="09:40:55"
          />
          <EventFeedItem
            status="idle"
            label="agent.started"
            message="Ready and waiting for the first task."
            timestamp="09:40:41"
          />
        </List>
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="status" note="Sets the colour of the dot.">
        {STATUS_SAMPLES.map(({ status, label, message }) => (
          <Option key={status} name={status} use={`The dot uses the ${status} colour.`}>
            <List separator>
              <EventFeedItem status={status} label={label} message={message} timestamp="09:41:12" />
            </List>
          </Option>
        ))}
      </OptionTable>
      <OptionTable title="meta">
        <Option
          name="meta"
          use="Adds a second identifier next to the label, such as an agent name."
        >
          <List separator>
            <EventFeedItem
              status="healthy"
              label="policy.allow"
              meta="apollo-retriever"
              message="Tool call approved within scope."
              timestamp="09:41:12"
            />
          </List>
        </Option>
        <Option name="no meta" use="Leave it out when the label says enough.">
          <List separator>
            <EventFeedItem
              status="healthy"
              label="policy.allow"
              message="Tool call approved within scope."
              timestamp="09:41:12"
            />
          </List>
        </Option>
      </OptionTable>
    </Stack>
  ),
};
