import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography';
import { SectionLabel } from './section-label';

const meta: Meta<typeof SectionLabel> = {
  title: 'Terra/Atoms/SectionLabel',
  component: SectionLabel,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof SectionLabel>;

/** The overline on its own. */
export const Default: Story = {
  render: () => <SectionLabel>Endpoint</SectionLabel>,
};

/**
 * What it is for: labelling regions of a dense panel. It organises without
 * competing, so the eye lands on the content rather than the label.
 */
export const InAPanel: Story = {
  render: () => (
    <Stack gap="comfortable" className="w-[360px]">
      <Stack gap="tight">
        <SectionLabel>Endpoint</SectionLabel>
        <Text size="caption" mono>
          https://api.orbit.example/v1/ingest/gh-main
        </Text>
      </Stack>
      <Stack gap="tight">
        <SectionLabel>Last delivery</SectionLabel>
        <Text size="caption" color="muted">
          4 minutes ago · 200 OK
        </Text>
      </Stack>
    </Stack>
  ),
};

/**
 * The same recipe (`sectionLabelVariants`) draws the DataGrid header and the
 * StatCard label, so every overline in Terra is one treatment rather than four
 * near-misses.
 */
export const OneTreatment: Story = {
  render: () => (
    <Stack gap="snug" className="w-[360px]">
      <SectionLabel>Active triggers</SectionLabel>
      <SectionLabel>Planned limits</SectionLabel>
      <SectionLabel>Audit</SectionLabel>
    </Stack>
  ),
};
