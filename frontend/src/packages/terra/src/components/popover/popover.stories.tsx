import type { Meta, StoryObj } from '@storybook/react';
import { Example, Frame, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Popover, PopoverContent, PopoverTrigger } from './popover';

/**
 * A small panel that opens next to a button and holds extra content or
 * controls. Clicking outside closes it. The samples here are held open so
 * you can see them.
 */
const meta: Meta<typeof Popover> = {
  title: 'Terra/Atoms/Popover',
  component: Popover,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Popover>;

const keepStill = (event: Event) => event.preventDefault();

function Stage({ children }: { children: React.ReactNode }) {
  return (
    <Frame className="flex min-h-56 items-center justify-center">
      <div className="flex justify-center">{children}</div>
    </Frame>
  );
}

function AgentNote({
  side,
  align,
}: {
  side?: 'top' | 'right' | 'bottom' | 'left';
  align?: 'start' | 'center' | 'end';
}) {
  return (
    <Popover open>
      <PopoverTrigger asChild>
        <Button variant="secondary" size="sm">
          Agent details
        </Button>
      </PopoverTrigger>
      <PopoverContent
        {...(side ? { side } : {})}
        {...(align ? { align } : {})}
        className="w-56 p-3"
        onOpenAutoFocus={keepStill}
        onInteractOutside={keepStill}
      >
        <Stack gap="tight">
          <Text size="supporting" weight="emphasis">
            Billing agent
          </Text>
          <Text size="caption" color="muted">
            Can read invoices. Cannot send email.
          </Text>
        </Stack>
      </PopoverContent>
    </Popover>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Popover with content">
        <Stage>
          <AgentNote />
        </Stage>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Side, alignment and offset go on PopoverContent. It moves to the other side on its own if there is no room."
      >
        <Option name='side="bottom"' use="Opens below the button. The default.">
          <Stage>
            <AgentNote side="bottom" />
          </Stage>
        </Option>
        <Option name='side="top"' use="Opens above the button.">
          <Stage>
            <AgentNote side="top" />
          </Stage>
        </Option>
        <Option name='side="right"' use="Opens to the right of the button.">
          <Stage>
            <AgentNote side="right" />
          </Stage>
        </Option>
        <Option name='side="left"' use="Opens to the left of the button.">
          <Stage>
            <AgentNote side="left" />
          </Stage>
        </Option>
        <Option
          name='align="start"'
          use="Lines the panel up with the start of the button. The default."
        >
          <Stage>
            <AgentNote align="start" />
          </Stage>
        </Option>
        <Option name='align="center"' use="Centres the panel on the button.">
          <Stage>
            <AgentNote align="center" />
          </Stage>
        </Option>
        <Option name='align="end"' use="Lines the panel up with the end of the button.">
          <Stage>
            <AgentNote align="end" />
          </Stage>
        </Option>
      </OptionTable>
    </div>
  ),
};

/** The content matches its trigger's width, with an 18rem floor for narrow triggers. */
export const MatchTriggerWidth: Story = {
  render: () => (
    <div className="w-[32rem]">
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="secondary" size="sm" fullWidth>
            Choose an event
          </Button>
        </PopoverTrigger>
        <PopoverContent matchTriggerWidth className="p-3">
          <Text size="supporting" color="foreground">
            As wide as the button that opened it.
          </Text>
        </PopoverContent>
      </Popover>
    </div>
  ),
};
