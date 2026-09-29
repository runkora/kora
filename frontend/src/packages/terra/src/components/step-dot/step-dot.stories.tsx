import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { StepConnector, StepDot } from './index';

/**
 * StepDot
 *
 * A numbered circle for a step in a sequence. Join dots with StepConnector to draw
 * the line between them.
 */
const meta: Meta<typeof StepDot> = {
  title: 'Terra/Atoms/StepDot',
  component: StepDot,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof StepDot>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="Setup guide">
        <div className="flex gap-4">
          <Stack align="center" gap="none">
            <StepDot>1</StepDot>
            <StepConnector />
            <StepDot>2</StepDot>
            <StepConnector />
            <StepDot variant="surface">3</StepDot>
          </Stack>
          <Stack gap="relaxed">
            <Stack gap="tight">
              <Text size="supporting" weight="emphasis">
                Create an API key
              </Text>
              <Text size="caption" color="muted">
                Open the Keys tab and create a key for the gateway.
              </Text>
            </Stack>
            <Stack gap="tight">
              <Text size="supporting" weight="emphasis">
                Point your agent at the gateway
              </Text>
              <Text size="caption" color="muted">
                Set the base URL to your Orbit endpoint.
              </Text>
            </Stack>
            <Stack gap="tight">
              <Text size="supporting" weight="emphasis">
                Check the connection
              </Text>
              <Text size="caption" color="muted">
                Send a test request and confirm it succeeds.
              </Text>
            </Stack>
          </Stack>
        </div>
      </Example>
      <Example title="Wizard progress">
        <Stack align="center" gap="none" className="h-56 w-fit">
          <StepDot size="lg" state="complete">
            1
          </StepDot>
          <StepConnector complete />
          <StepDot size="lg" state="current">
            2
          </StepDot>
          <StepConnector />
          <StepDot size="lg" state="pending">
            3
          </StepDot>
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl space-y-10">
      <OptionTable title="StepDot">
        <Option name='variant="accent"' use="Filled with the brand colour. The default.">
          <StepDot variant="accent">1</StepDot>
        </Option>
        <Option
          name='variant="surface"'
          use="Quiet grey, for steps not yet reached or a rank number."
        >
          <StepDot variant="surface">2</StepDot>
        </Option>
        <Option name='size="sm"' use="16 px. A rank number in a list.">
          <StepDot size="sm">1</StepDot>
        </Option>
        <Option name='size="md"' use="24 px. The default.">
          <StepDot size="md">1</StepDot>
        </Option>
        <Option name='size="lg"' use="30 px with a code typeface. A wizard rail.">
          <StepDot size="lg">1</StepDot>
        </Option>
        <Option name='state="pending"' use="Outlined. A step not reached yet. Replaces variant.">
          <StepDot size="lg" state="pending">
            3
          </StepDot>
        </Option>
        <Option name='state="current"' use="Filled. The step the user is on.">
          <StepDot size="lg" state="current">
            2
          </StepDot>
        </Option>
        <Option name='state="complete"' use="Softly tinted. A finished step.">
          <StepDot size="lg" state="complete">
            1
          </StepDot>
        </Option>
      </OptionTable>
      <OptionTable title="StepConnector">
        <Option name="default" use="A grey line that fills the space between two dots.">
          <Stack align="center" gap="none" className="h-20 w-fit">
            <StepDot>1</StepDot>
            <StepConnector />
            <StepDot>2</StepDot>
          </Stack>
        </Option>
        <Option name="complete" use="Tint the line to mark the part after a finished step.">
          <Stack align="center" gap="none" className="h-20 w-fit">
            <StepDot state="complete">1</StepDot>
            <StepConnector complete />
            <StepDot state="current">2</StepDot>
          </Stack>
        </Option>
        <Option name='orientation="horizontal"' use="Run the line sideways, for a row of steps.">
          <div className="flex w-64 items-center">
            <StepDot state="complete">1</StepDot>
            <StepConnector orientation="horizontal" complete />
            <StepDot state="current">2</StepDot>
            <StepConnector orientation="horizontal" />
            <StepDot state="pending">3</StepDot>
          </div>
        </Option>
      </OptionTable>
    </div>
  ),
};
