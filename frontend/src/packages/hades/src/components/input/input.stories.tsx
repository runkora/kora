import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Input } from './input';
import { Label } from '../label/label';

/**
 * Input
 *
 * A single-line text box. Pair it with a Label so people know what to type.
 */
const meta: Meta<typeof Input> = {
  title: 'Hades/Atoms/Input',
  component: Input,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Input>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-8">
      <Example title="With a label">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="overview-name">Agent name</Label>
          <Input id="overview-name" placeholder="policy-auditor" />
        </div>
      </Example>
      <Example title="With a value">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="overview-email">Email</Label>
          <Input id="overview-email" type="email" defaultValue="dev@runkora.test" />
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="default" use="An empty field with placeholder text.">
          <Input aria-label="Agent name" placeholder="Agent name" />
        </Option>
        <Option name="disabled" use="The field cannot be edited.">
          <Input aria-label="Agent name, disabled" disabled defaultValue="policy-auditor" />
        </Option>
        <Option name="hasError" use="Marks the value as invalid. Show a message next to it.">
          <Input aria-label="Email, invalid" hasError defaultValue="not-an-email" />
        </Option>
        <Option name="readOnly" use="The value can be selected and copied but not changed.">
          <Input aria-label="Agent ID, read only" readOnly defaultValue="agt_01HXYZ" />
        </Option>
        <Option name="mono" use="Fixed-width text for keys, paths and other technical values.">
          <Input aria-label="Path" mono defaultValue="/api/v1/agents/:id" />
        </Option>
        <Option name="compact" use="A shorter field, to line up with small buttons or selects.">
          <Input aria-label="Search" compact placeholder="Search" />
        </Option>
        <Option
          name="startAdornment"
          use="Fixed text inside the left edge, such as a currency sign."
        >
          <Input aria-label="Input price" type="number" startAdornment="$" defaultValue="3" />
        </Option>
        <Option name="endAdornment" use="Fixed text inside the right edge, such as a unit.">
          <Input aria-label="Output price" type="number" endAdornment="/ 1M" defaultValue="15" />
        </Option>
        <Option name='type="password"' use="Hides what is typed. Other types work the same way.">
          <Input aria-label="Password" type="password" defaultValue="secret-value" />
        </Option>
      </OptionTable>
    </div>
  ),
};
