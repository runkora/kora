import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Text } from '../typography/typography';
import { StatusDot } from './status-dot';

/**
 * StatusDot
 *
 * A small coloured circle that shows the state of something. Put a text label beside it,
 * because colour alone is not enough.
 */
const meta: Meta<typeof StatusDot> = {
  title: 'Hades/Atoms/StatusDot',
  component: StatusDot,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof StatusDot>;

const AGENTS = [
  { status: 'healthy', label: 'Support agent running' },
  { status: 'degraded', label: 'Billing agent slow' },
  { status: 'error', label: 'Deploy agent failed' },
  { status: 'idle', label: 'Report agent idle' },
  { status: 'pending', label: 'Triage agent starting' },
] as const;

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="Beside a label">
        <div className="space-y-3">
          {AGENTS.map(({ status, label }) => (
            <div key={status} className="flex items-center gap-2">
              <StatusDot status={status} glow={status === 'healthy'} pulse={status === 'pending'} />
              <Text size="supporting">{label}</Text>
            </div>
          ))}
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl space-y-10">
      <OptionTable title="Status">
        <Option name='status="healthy"' use="Working normally. The default.">
          <StatusDot status="healthy" />
        </Option>
        <Option name='status="degraded"' use="Working, with warnings.">
          <StatusDot status="degraded" />
        </Option>
        <Option name='status="error"' use="Something failed.">
          <StatusDot status="error" />
        </Option>
        <Option name='status="idle"' use="Not running and not scheduled.">
          <StatusDot status="idle" />
        </Option>
        <Option name='status="pending"' use="Starting up or waiting for a decision.">
          <StatusDot status="pending" />
        </Option>
        <Option name='status="muted"' use="Switched off. Says nothing about health.">
          <StatusDot status="muted" />
        </Option>
        <Option name='status="accent"' use="The brand colour, for example in a chart key.">
          <StatusDot status="accent" />
        </Option>
      </OptionTable>
      <OptionTable title="Size and effects">
        <Option name='size="sm"' use="6 px. Inside a table cell or a badge.">
          <StatusDot size="sm" />
        </Option>
        <Option name='size="md"' use="10 px. The default.">
          <StatusDot size="md" />
        </Option>
        <Option name='size="lg"' use="12 px. In a header or a large card.">
          <StatusDot size="lg" />
        </Option>
        <Option name="glow" use="Add a soft ring so a live state stands out. Not used with muted.">
          <div className="flex items-center gap-4">
            <StatusDot status="healthy" glow />
            <StatusDot status="error" glow />
          </div>
        </Option>
        <Option name="pulse" use="Fade in and out to show something is in progress.">
          <StatusDot status="pending" glow pulse />
        </Option>
      </OptionTable>
    </div>
  ),
};
