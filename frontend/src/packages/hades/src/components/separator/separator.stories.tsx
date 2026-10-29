import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Separator } from './separator';

/**
 * Separator
 *
 * A thin line that divides content. It can run across or down and always spans
 * the full length of the space it sits in.
 */
const meta: Meta<typeof Separator> = {
  title: 'Hades/Atoms/Separator',
  component: Separator,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Separator>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="Between sections">
        <Stack gap="comfortable">
          <Text>Agents run your tasks.</Text>
          <Separator />
          <Text>Policies decide what they can do.</Text>
        </Stack>
      </Example>
      <Example title="Between inline items">
        <Stack direction="horizontal" gap="cozy" align="center">
          <Text size="supporting" color="muted">
            Agents
          </Text>
          <div className="h-4">
            <Separator orientation="vertical" />
          </div>
          <Text size="supporting" color="muted">
            Policies
          </Text>
          <div className="h-4">
            <Separator orientation="vertical" />
          </div>
          <Text size="supporting" color="muted">
            Events
          </Text>
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='orientation="horizontal"' use="A line across. The default.">
          <Separator />
        </Option>
        <Option name='orientation="vertical"' use="A line down. Give its parent a height.">
          <div className="h-10">
            <Separator orientation="vertical" />
          </div>
        </Option>
        <Option
          name='tone="strong"'
          use="The standard border colour instead of the subtle one. Use where the line is lost beside bordered controls."
        >
          <Separator tone="strong" />
        </Option>
        <Option
          name="decorative={false}"
          use="Tell screen readers this is a real divider. Leave it on for purely visual lines."
        >
          <Separator decorative={false} />
        </Option>
      </OptionTable>
    </div>
  ),
};
