import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { CopyField } from './copy-field';

/**
 * CopyField
 *
 * A read-only value that copies when the whole field is clicked. It works as
 * an input-style field or as a detail row with a caption above the value.
 */
const meta: Meta<typeof CopyField> = {
  title: 'Hades/Molecules/CopyField',
  component: CopyField,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof CopyField>;

const KEY = 'orb_3f9a1c7e5b2d48a6b0e4c1d9f7a2836e';
const LONG =
  'us.anthropic.claude-sonnet-4-5-20250929-v1:0:with-a-very-long-suffix-that-would-not-fit-on-one-line';

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="API key">
        <CopyField value={KEY} mono ariaLabel="Copy API key to clipboard" />
      </Example>
      <Example title="Detail rows">
        <CopyField label="Request ID" value="req_8d1f2c" mono />
        <CopyField label="Provider" value="Anthropic" />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Every sample is live. A border appears by default only for a bare value."
      >
        <Option name="value" use="The plainest form: a bordered field that truncates long text.">
          <CopyField value={KEY} />
        </Option>
        <Option name="mono" use="Use a monospace font for identifiers and keys.">
          <CopyField value={KEY} mono />
        </Option>
        <Option name="label" use="Adds a caption above the value and drops the border.">
          <CopyField label="Principal ID" value="usr_42b7e9" mono />
        </Option>
        <Option name="wrap" use="Lets a long value run onto several lines instead of truncating.">
          <CopyField label="Provider model ID" value={LONG} mono wrap />
        </Option>
        <Option name="bordered" use="Force the border on or off, whatever else is set.">
          <CopyField label="Email" value="dev@example.com" bordered />
        </Option>
        <Option name="fullWidth" use="Turn off to size the field to its content.">
          <CopyField value="usr_42b7e9" mono bordered={false} fullWidth={false} />
        </Option>
        <Option name="ariaLabel" use="Overrides the accessible name, which is otherwise derived.">
          <CopyField value={KEY} mono ariaLabel="Copy API key to clipboard" />
        </Option>
      </OptionTable>
    </div>
  ),
};
