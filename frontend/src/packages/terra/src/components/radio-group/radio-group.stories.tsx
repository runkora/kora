import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Label } from '../label/label';
import { RadioGroup, RadioGroupItem } from './index';

/**
 * RadioGroup
 *
 * A short list where exactly one option can be chosen. Each item is only the
 * circle, so put a Label next to it.
 */
const meta: Meta<typeof RadioGroup> = {
  title: 'Terra/Atoms/RadioGroup',
  component: RadioGroup,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof RadioGroup>;

function Row({
  id,
  value,
  label,
  disabled,
}: {
  id: string;
  value: string;
  label: string;
  disabled?: boolean;
}) {
  return (
    <div className="flex items-center gap-2">
      <RadioGroupItem id={id} value={value} disabled={disabled} />
      <Label htmlFor={id}>{label}</Label>
    </div>
  );
}

function Controlled() {
  const [value, setValue] = React.useState('claude-code');
  return (
    <RadioGroup aria-label="Framework" value={value} onValueChange={setValue}>
      <Row id="overview-claude" value="claude-code" label="Claude Code" />
      <Row id="overview-codex" value="codex" label="Codex" />
      <Row id="overview-custom" value="custom" label="Custom" />
    </RadioGroup>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-8">
      <Example title="Choose one">
        <Controlled />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="unselected" use="A circle that is not chosen.">
          <RadioGroup aria-label="Unselected" defaultValue="">
            <Row id="opt-unselected" value="a" label="Codex" />
          </RadioGroup>
        </Option>
        <Option name="selected" use="The chosen option, filled in.">
          <RadioGroup aria-label="Selected" defaultValue="a">
            <Row id="opt-selected" value="a" label="Claude Code" />
          </RadioGroup>
        </Option>
        <Option name="disabled" use="Cannot be chosen.">
          <RadioGroup aria-label="Disabled" defaultValue="">
            <Row id="opt-disabled" value="a" label="Not available" disabled />
          </RadioGroup>
        </Option>
        <Option name="disabled, selected" use="Chosen and locked.">
          <RadioGroup aria-label="Disabled and selected" defaultValue="a">
            <Row id="opt-disabled-selected" value="a" label="Set by policy" disabled />
          </RadioGroup>
        </Option>
      </OptionTable>
    </div>
  ),
};
