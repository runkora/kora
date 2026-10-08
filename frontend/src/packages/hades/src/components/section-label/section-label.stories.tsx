import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Stack } from '../layout/stack';
import { Text } from '../typography';
import { SectionLabel } from './section-label';

/**
 * SectionLabel
 *
 * A small all-caps label that names a block inside a dense panel. Use Heading
 * when the title needs to stand on its own.
 */
const meta: Meta<typeof SectionLabel> = {
  title: 'Hades/Atoms/SectionLabel',
  component: SectionLabel,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof SectionLabel>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="In a detail panel">
        <Stack gap="comfortable">
          <Stack gap="tight">
            <SectionLabel>Endpoint</SectionLabel>
            <Text size="caption" mono>
              https://api.orbit.example/v1/ingest/gh-main
            </Text>
          </Stack>
          <Stack gap="tight">
            <SectionLabel>Last delivery</SectionLabel>
            <Text size="caption" color="muted">
              4 minutes ago, 200 OK
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
        note="SectionLabel has no variants. It is one look: 10px, semibold, capitals, subtle colour."
      >
        <Option
          name="children"
          use="The label text. Write it in normal case; capitals are applied."
        >
          <SectionLabel>Active triggers</SectionLabel>
        </Option>
        <Option
          name="sectionLabelVariants()"
          use="The same look as a class string, for places that cannot use the component, such as a table header."
        >
          <Text size="supporting" color="muted">
            Grid headers and stat card labels use it.
          </Text>
        </Option>
      </OptionTable>
    </div>
  ),
};
