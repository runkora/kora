import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { CopyButton } from './copy-button';

/**
 * CopyButton
 *
 * A small icon button that copies a value to the clipboard. The icon turns
 * into a tick for a moment so the click feels acknowledged.
 */
const meta: Meta<typeof CopyButton> = {
  title: 'Hades/Atoms/CopyButton',
  component: CopyButton,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof CopyButton>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Beside a value">
        <Stack direction="horizontal" align="center" gap="snug">
          <Text size="supporting" mono>
            https://api.example.com/v1/hooks/abc123
          </Text>
          <CopyButton value="https://api.example.com/v1/hooks/abc123" label="Copy webhook URL" />
        </Stack>
        <Text size="supporting" color="muted">
          Click it to copy the address. The tick shows for a moment, then the icon returns.
        </Text>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options" note="Every sample is live.">
        <Option name="label" use="Names the button for screen readers. Say what is being copied.">
          <CopyButton value="orb_example" label="Copy API key" />
        </Option>
        <Option name="default label" use="Without a label the button is named 'Copy to clipboard'.">
          <CopyButton value="orb_example" />
        </Option>
      </OptionTable>
    </div>
  ),
};
