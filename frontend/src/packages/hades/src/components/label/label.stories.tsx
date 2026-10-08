import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Input } from '../input/input';
import { Label } from './label';

/**
 * Label
 *
 * The name of a form control. Clicking it focuses the control.
 */
const meta: Meta<typeof Label> = {
  title: 'Hades/Atoms/Label',
  component: Label,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Label>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-8">
      <Example title="Above an input">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="overview-agent">Agent name</Label>
          <Input id="overview-agent" placeholder="policy-auditor" />
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="htmlFor" use="Links the label to the control with the same id.">
          <div className="flex max-w-xs flex-col gap-1.5">
            <Label htmlFor="opt-email">Email</Label>
            <Input id="opt-email" type="email" placeholder="you@example.com" />
          </div>
        </Option>
      </OptionTable>
    </div>
  ),
};
