import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Eyebrow } from './eyebrow';

/**
 * Eyebrow
 *
 * A small all-caps label above a heading or at the top of a card section. New
 * code should use SectionLabel; Eyebrow stays for inline use.
 */
const meta: Meta<typeof Eyebrow> = {
  title: 'Terra/Atoms/Eyebrow',
  component: Eyebrow,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Eyebrow>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="Above supporting text">
        <Stack gap="tight">
          <Eyebrow>Audit</Eyebrow>
          <Text size="supporting" color="muted">
            Saving re-provisions the agent and recompiles its grants before they apply.
          </Text>
        </Stack>
      </Example>
      <Example title="Dividing a side panel">
        <Stack gap="comfortable">
          <Stack gap="tight">
            <Eyebrow>Planned limits</Eyebrow>
            <Text size="supporting" color="muted">
              Requests per hour, spend per day.
            </Text>
          </Stack>
          <Stack gap="tight">
            <Eyebrow>Connections</Eyebrow>
            <Text size="supporting" color="muted">
              Outbound hosts this agent may reach.
            </Text>
          </Stack>
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Eyebrow has no variants. It is one look: 11px, bold, capitals, subtle colour."
      >
        <Option
          name="children"
          use="The label text. Write it in normal case; capitals are applied."
        >
          <Eyebrow>Audit</Eyebrow>
        </Option>
        <Option name="Inline" use="It is a span, so it can sit in a line beside other content.">
          <Stack direction="horizontal" gap="snug" align="center">
            <Eyebrow>Status</Eyebrow>
            <Text size="supporting">Running</Text>
          </Stack>
        </Option>
      </OptionTable>
    </div>
  ),
};
