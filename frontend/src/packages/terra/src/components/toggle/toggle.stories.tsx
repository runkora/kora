import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Text } from '../typography/typography';
import { Toggle } from './toggle';

/**
 * Toggle
 *
 * An on/off switch for a setting that takes effect straight away.
 */
const meta: Meta<typeof Toggle> = {
  title: 'Terra/Atoms/Toggle',
  component: Toggle,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Toggle>;

function Labelled({ label, initial = false }: { label: string; initial?: boolean }) {
  const [on, setOn] = React.useState(initial);
  const id = React.useId();
  return (
    <div className="flex items-center justify-between gap-4">
      <Text as="span" id={id}>
        {label}
      </Text>
      <Toggle checked={on} onChange={setOn} aria-labelledby={id} />
    </div>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-8">
      <Example title="Beside a label">
        <div className="flex flex-col gap-3">
          <Labelled label="Enable model" initial />
          <Labelled label="Send weekly summary" />
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="checked={false}" use="Off.">
          <Toggle checked={false} onChange={() => {}} aria-label="Off" />
        </Option>
        <Option name="checked" use="On.">
          <Toggle checked onChange={() => {}} aria-label="On" />
        </Option>
        <Option name="disabled, off" use="Cannot be changed.">
          <Toggle checked={false} disabled onChange={() => {}} aria-label="Off, disabled" />
        </Option>
        <Option name="disabled, on" use="Cannot be changed.">
          <Toggle checked disabled onChange={() => {}} aria-label="On, disabled" />
        </Option>
        <Option name="aria-label" use="Names the switch when there is no visible text beside it.">
          <Toggle checked onChange={() => {}} aria-label="Enable model" />
        </Option>
      </OptionTable>
    </div>
  ),
};
