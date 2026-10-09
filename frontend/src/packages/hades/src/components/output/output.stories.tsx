import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Input } from '../input/input';
import { Label } from '../label/label';
import { Text } from '../typography/typography';
import { Field } from '../field/field';
import { Output } from './output';

/**
 * Output
 *
 * Shows a value that the page worked out, such as a total or a result. It
 * can be linked to the inputs the value comes from.
 */
const meta: Meta<typeof Output> = {
  title: 'Hades/Atoms/Output',
  component: Output,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Output>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-6">
      <Example title="Beside the inputs it comes from">
        <Field>
          <Label htmlFor="overview-runs">Runs per day</Label>
          <Input id="overview-runs" defaultValue="40" />
        </Field>
        <Field>
          <Text size="supporting" weight="heavy">
            Runs per month
          </Text>
          <Output field htmlFor="overview-runs">
            1,200
          </Output>
        </Field>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="default" use="Plain text with no box. Sits inline with the text around it.">
          <Text>
            Estimated cost: <Output htmlFor="options-runs">$12.40</Output>
          </Text>
        </Option>
        <Option
          name="field"
          use="Looks like an input, for a read-only value next to editable ones."
        >
          <Output field htmlFor="options-runs">
            $12.40
          </Output>
        </Option>
        <Option
          name="htmlFor"
          use="Names the input ids the value comes from. Screen readers use it to link them."
        >
          <Field>
            <Label htmlFor="options-runs">Runs</Label>
            <Input id="options-runs" defaultValue="40" />
            <Output field htmlFor="options-runs">
              $12.40
            </Output>
          </Field>
        </Option>
      </OptionTable>
    </div>
  ),
};
