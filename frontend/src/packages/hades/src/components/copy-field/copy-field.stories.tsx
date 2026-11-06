import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { FieldRow } from '../field-row/field-row';
import { Input } from '../input/input';
import { Text } from '../typography/typography';
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
  argTypes: {
    value: { control: 'text', description: 'The string shown and copied.' },
    label: { control: 'text', description: 'Caption above the value. Drops the border.' },
    ariaLabel: { control: 'text', description: 'Overrides the derived accessible name.' },
    mono: { control: 'boolean' },
    filled: { control: 'boolean', description: 'The recessed FieldRow surface.' },
    bordered: { control: 'boolean', description: 'Force the border on or off.' },
    wrap: { control: 'boolean', description: 'Wrap a long value instead of truncating.' },
    fullWidth: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof CopyField>;

/** The fill is `bg-background`, the docs canvas colour, so `filled` samples sit on a surface to show it. */
const OnSurface = ({ children }: { children: React.ReactNode }) => (
  <div className="rounded-lg bg-surface p-3">{children}</div>
);

const KEY = 'kora_3f9a1c7e5b2d48a6b0e4c1d9f7a2836e';
const LONG =
  'us.anthropic.claude-sonnet-4-5-20250929-v1:0:with-a-very-long-suffix-that-would-not-fit-on-one-line';

/** Every prop as a control, on a surface so `filled` shows. */
export const Playground: Story = {
  args: { value: KEY, mono: true, fullWidth: true },
  render: (args) => (
    <div className="max-w-md rounded-lg bg-surface p-4">
      <CopyField {...args} />
    </div>
  ),
};

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
        <Option
          name="filled"
          use="Sits on a recessed surface with the same subtle border, so it stays visible on any background."
        >
          <OnSurface>
            <CopyField value={KEY} mono filled />
          </OnSurface>
        </Option>
        <Option name="filled + bordered={false}" use="The recessed surface with no border.">
          <OnSurface>
            <CopyField value={KEY} mono filled bordered={false} />
          </OnSurface>
        </Option>
        <Option name="bordered" use="Force the border on or off, whatever else is set.">
          <CopyField label="Email" value="dev@example.com" bordered />
        </Option>
        <Option name="fullWidth" use="Turn off to size the field to its content.">
          <CopyField label="Region" value="ap-southeast-2" mono fullWidth={false} />
        </Option>
        <Option
          name="ariaLabel"
          use="Overrides the accessible name. By default it is 'Copy {label}: {value}', so a screen reader hears the value."
        >
          <CopyField value={KEY} mono ariaLabel="Copy API key to clipboard" />
        </Option>
      </OptionTable>
    </div>
  ),
};

const SURFACES = [
  ['background', 'bg-background'],
  ['surface', 'bg-surface'],
  ['surface-elevated', 'bg-surface-elevated'],
] as const;

/**
 * The default field is transparent, so it takes the colour behind it and only
 * its border separates it. `filled` keeps its own surface. Switch the theme in
 * the toolbar to check both.
 */
export const Surfaces: Story = {
  render: () => (
    <div className="grid max-w-3xl grid-cols-1 gap-4 md:grid-cols-2">
      {SURFACES.map(([name, bg]) => (
        <div key={name} className={`${bg} space-y-3 rounded-lg border border-border-subtle p-4`}>
          <Text size="caption" color="muted">
            {name}
          </Text>
          <CopyField value={KEY} mono />
          <CopyField value={KEY} mono filled />
        </div>
      ))}
    </div>
  ),
};

/** `filled` against the controls it is meant to match. */
export const BesideInputs: Story = {
  render: () => (
    <div className="max-w-md space-y-3 rounded-lg bg-surface p-4">
      <Input aria-label="Name" defaultValue="github-push" />
      <CopyField value="https://api.example.com/api/v1/webhooks/c5c8aa68" mono filled />
      <FieldRow>
        <Text mono size="caption">
          shown once at creation
        </Text>
      </FieldRow>
    </div>
  ),
};

const stubClipboard = (writeText: () => Promise<void>) =>
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });

/** Clicking anywhere on the field copies it and swaps the icon for a check. */
export const Copied: Story = {
  render: () => (
    <div className="max-w-md">
      <CopyField label="Request ID" value="req_8d1f2c" mono />
    </div>
  ),
  play: async ({ canvasElement }) => {
    stubClipboard(() => Promise.resolve());
    await userEvent.click(await within(canvasElement).findByRole('button'));
  },
};

/** A refused clipboard write shows an error icon instead. */
export const CopyFailed: Story = {
  render: () => (
    <div className="max-w-md">
      <CopyField label="Request ID" value="req_8d1f2c" mono />
    </div>
  ),
  play: async ({ canvasElement }) => {
    stubClipboard(() => Promise.reject(new Error('denied')));
    await userEvent.click(await within(canvasElement).findByRole('button'));
  },
};
