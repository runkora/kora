import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Checkbox } from '../checkbox/checkbox';
import { Stack } from '../layout/stack';
import { ToggleRow } from './toggle-row';

/**
 * ToggleRow
 *
 * A setting that is on or off. The whole row is the switch. Pass a control instead to pair a label with your own checkbox.
 */
const meta: Meta<typeof ToggleRow> = {
  title: 'Hades/Molecules/ToggleRow',
  component: ToggleRow,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof ToggleRow>;

function SettingsList() {
  const [stream, setStream] = useState(true);
  const [retry, setRetry] = useState(false);
  const [context, setContext] = useState(true);
  return (
    <Stack gap="snug">
      <ToggleRow
        checked={stream}
        onChange={setStream}
        label="Streaming responses"
        description="Send text as it is written instead of all at once."
      />
      <ToggleRow
        checked={retry}
        onChange={setRetry}
        label="Retry failed tool calls"
        description="Try up to 3 times before showing the error."
      />
      <ToggleRow
        checked={context}
        onChange={setContext}
        label="Keep context between sessions"
        description="Remember earlier messages and tool results."
      />
    </Stack>
  );
}

function CheckboxRow() {
  const [checked, setChecked] = useState(false);
  return (
    <ToggleRow
      htmlFor="overview-terms"
      label="Keep conversation history"
      description="Save tool calls between sessions."
      control={
        <Checkbox id="overview-terms" checked={checked} onChange={(value) => setChecked(value)} />
      }
    />
  );
}

function Switch({
  start,
  ...rest
}: {
  start: boolean;
  label: string;
  description?: string;
  disabled?: boolean;
}) {
  const [on, setOn] = useState(start);
  return <ToggleRow checked={on} onChange={setOn} {...rest} />;
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-lg space-y-8">
      <Example title="A settings list">
        <SettingsList />
      </Example>
      <Example title="With your own checkbox">
        <CheckboxRow />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="checked: false" use="Off. The text is dimmed.">
          <Switch
            start={false}
            label="Streaming responses"
            description="Send text as it is written."
          />
        </Option>
        <Option
          name="checked: true"
          use="On. The switch is coloured and the text is at full strength."
        >
          <Switch start label="Streaming responses" description="Send text as it is written." />
        </Option>
        <Option name="no description" use="A single line.">
          <Switch start label="Retry on failure" />
        </Option>
        <Option name="disabled" use="Dimmed. It cannot be changed by mouse or keyboard.">
          <Switch
            start={false}
            disabled
            label="Tool access"
            description="Needs at least one tool server."
          />
        </Option>
        <Option
          name="control"
          use="Shows your own checkbox or radio beside the label, with no card around it. Use inside forms."
        >
          <ToggleRow
            htmlFor="opt-agree"
            label="I agree to the terms"
            control={<Checkbox id="opt-agree" />}
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
