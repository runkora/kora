import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Checkbox } from './checkbox';

/**
 * Checkbox
 *
 * A tick box with an optional label. Use it for choices that can be on or off
 * independently of each other.
 */
const meta: Meta<typeof Checkbox> = {
  title: 'Hades/Atoms/Checkbox',
  component: Checkbox,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Checkbox>;

function Controlled({ label, initial = false }: { label: string; initial?: boolean }) {
  const [checked, setChecked] = React.useState(initial);
  return (
    <Checkbox checked={checked} onChange={setChecked}>
      {label}
    </Checkbox>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-sm space-y-8">
      <Example title="Single choice">
        <Controlled label="Send email notifications" initial />
      </Example>
      <Example title="A list of choices">
        <div className="flex flex-col gap-3">
          <Controlled label="Read files" initial />
          <Controlled label="Write files" />
          <Controlled label="Run commands" />
        </div>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="checked={false}" use="Not selected.">
          <Checkbox checked={false} onChange={() => {}}>
            Read files
          </Checkbox>
        </Option>
        <Option name="checked" use="Selected.">
          <Checkbox checked onChange={() => {}}>
            Read files
          </Checkbox>
        </Option>
        <Option
          name="indeterminate"
          use="Some, but not all, of a group is selected. Use it on a select-all box."
        >
          <Checkbox checked={false} indeterminate onChange={() => {}}>
            Select all tools
          </Checkbox>
        </Option>
        <Option name="disabled" use="Cannot be changed.">
          <Checkbox checked disabled onChange={() => {}}>
            Required by policy
          </Checkbox>
        </Option>
        <Option name='size="md"' use="The standard 20px box. The default.">
          <Checkbox checked onChange={() => {}}>
            Medium
          </Checkbox>
        </Option>
        <Option name='size="lg"' use="A larger 22px box, easier to hit on touch screens.">
          <Checkbox size="lg" checked onChange={() => {}}>
            Large
          </Checkbox>
        </Option>
        <Option
          name='variant="row"'
          use="A full-width row that highlights on hover. Good for long lists."
        >
          <div className="max-w-xs">
            <Checkbox variant="row" paddingBlock="snug" checked={false} onChange={() => {}}>
              github-mcp
            </Checkbox>
          </div>
        </Option>
        <Option
          name="aria-label"
          use="A box with no visible text still needs a name for screen readers."
        >
          <Checkbox aria-label="Select server" checked={false} onChange={() => {}} />
        </Option>
      </OptionTable>
    </div>
  ),
};
