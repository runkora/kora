import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '../button/button';
import { Icon } from '../icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography';
import { FieldRow } from './field-row';

const meta: Meta<typeof FieldRow> = {
  title: 'Terra/Molecules/FieldRow',
  component: FieldRow,
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
type Story = StoryObj<typeof FieldRow>;

/** A value to read or copy, not to edit — recessed so it never reads as an input. */
export const Default: Story = {
  render: () => (
    <div className="w-[460px]">
      <FieldRow
        leading={<Icon name="webhook" size="2xs" color="subtle" />}
        actions={
          <Button variant="ghost" size="xs">
            <Icon name="copy" size="2xs" />
            Copy
          </Button>
        }
      >
        <Text size="caption" mono>
          https://api.orbit.example/v1/ingest/gh-main
        </Text>
      </FieldRow>
    </div>
  ),
};

/** No leading marker and no actions: the value alone. */
export const ValueOnly: Story = {
  render: () => (
    <div className="w-[460px]">
      <FieldRow>
        <Text size="caption" mono>
          trg_01HZ9K3VQ8N2P4
        </Text>
      </FieldRow>
    </div>
  ),
};

/** A masked secret, with the actions that operate on it. */
export const Secret: Story = {
  render: () => (
    <div className="w-[460px]">
      <FieldRow
        leading={<Icon name="lock" size="2xs" color="subtle" />}
        actions={
          <>
            <Button variant="ghost" size="xs">
              <Icon name="eye" size="2xs" />
              Reveal
            </Button>
            <Button variant="ghost" size="xs">
              <Icon name="refresh" size="2xs" />
              Rotate
            </Button>
          </>
        }
      >
        <Text size="caption" mono color="muted">
          whsec_••••••••••••••••••••
        </Text>
      </FieldRow>
    </div>
  ),
};

/** Long values truncate rather than wrapping, so a stack of rows stays even. */
export const Stacked: Story = {
  render: () => (
    <Stack gap="snug" className="w-[460px]">
      <FieldRow leading={<Icon name="globe" size="2xs" color="subtle" />}>
        <Text size="caption" mono>
          https://api.orbit.example/v1/ingest/a-very-long-source-slug-that-will-not-fit
        </Text>
      </FieldRow>
      <FieldRow leading={<Icon name="key" size="2xs" color="subtle" />}>
        <Text size="caption" mono>
          sha256=8f2e1c…
        </Text>
      </FieldRow>
    </Stack>
  ),
};
