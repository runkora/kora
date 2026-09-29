import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Checkbox } from './checkbox';

const meta: Meta<typeof Checkbox> = {
  title: 'Terra/Atoms/Checkbox',
  component: Checkbox,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
  argTypes: {
    variant: {
      control: 'select',
      options: ['default', 'row'],
    },
    checked: { control: 'boolean' },
    indeterminate: { control: 'boolean' },
    disabled: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof Checkbox>;

export const Default: Story = {
  args: { checked: true, children: 'Enable notifications' },
};

/** `indeterminate` is used for select-all checkboxes when a subset of items is selected. */
export const Indeterminate: Story = {
  args: { checked: false, indeterminate: true, children: 'Select all' },
};

/** `variant="row"` stretches to full width with a hover highlight and bottom border — used in list pickers. */
export const RowVariant: Story = {
  name: 'Row variant',
  render: () => (
    <div style={{ width: 320 }}>
      <Stack gap="none">
        <Checkbox variant="row" checked={true} paddingBlock="tight" paddingInline="snug">
          <Text size="caption">platform:grants:read</Text>
        </Checkbox>
        <Checkbox variant="row" checked={false} paddingBlock="tight" paddingInline="snug">
          <Text size="caption">platform:grants:write</Text>
        </Checkbox>
        <Checkbox
          variant="row"
          checked={false}
          indeterminate={true}
          paddingBlock="tight"
          paddingInline="snug"
        >
          <Text size="caption" color="subtle">
            Select all platform scopes
          </Text>
        </Checkbox>
      </Stack>
    </div>
  ),
};

/** Sizes side by side. `md` is the default and `lg` is used on selectable cards, such as the MCP server list. */
export const Sizes: Story = {
  render: () => (
    <Stack gap="comfortable">
      {(['md', 'lg'] as const).map((size) => (
        <Stack key={size} direction="horizontal" gap="relaxed" align="center">
          <Text size="caption" color="muted" mono>
            {size}
          </Text>
          <Checkbox size={size} checked={false} aria-label={`${size} unchecked`} />
          <Checkbox size={size} checked={true} aria-label={`${size} checked`} />
          <Checkbox size={size} checked={false} indeterminate={true} aria-label={`${size} mixed`} />
          <Checkbox size={size} checked={true} disabled={true} aria-label={`${size} disabled`} />
        </Stack>
      ))}
    </Stack>
  ),
};

/** Disabled state — pointer events removed, opacity reduced. */
export const Disabled: Story = {
  args: { checked: true, disabled: true, children: 'Read-only setting' },
};
