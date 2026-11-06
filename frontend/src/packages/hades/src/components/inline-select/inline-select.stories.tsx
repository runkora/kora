import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { InlineSelect } from './inline-select';

/**
 * InlineSelect
 *
 * A short menu of choices behind an inline word. Arrow keys move through the
 * options; Enter selects; Escape closes.
 */
const meta: Meta<typeof InlineSelect> = {
  title: 'Hades/Molecules/InlineSelect',
  component: InlineSelect,
  tags: ['autodocs'],
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof InlineSelect>;

const OPERATORS = [
  { value: 'is', label: 'is' },
  { value: 'contains', label: 'contains' },
];

function Controlled() {
  const [value, setValue] = useState('is');
  return (
    <InlineSelect
      aria-label="Operator"
      value={value}
      options={OPERATORS}
      onValueChange={setValue}
    />
  );
}

export const Default: Story = { render: () => <Controlled /> };

export const Placeholder: Story = {
  args: {
    'aria-label': 'Operator',
    value: '',
    options: OPERATORS,
    placeholder: 'Choose',
    onValueChange: () => {},
  },
};
