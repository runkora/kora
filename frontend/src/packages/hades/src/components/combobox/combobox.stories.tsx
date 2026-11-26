import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Popover, PopoverContent, PopoverTrigger } from '../popover/popover';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Combobox, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList } from './combobox';

/**
 * A list of options to search and pick from. Arrow keys move the highlight and Enter picks it.
 * The search matches each option's value and keywords. A disabled option stays in the list.
 */
const meta: Meta<typeof Combobox> = {
  title: 'Hades/Molecules/Combobox',
  component: Combobox,
  tags: ['autodocs'],
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof Combobox>;

const REGIONS = [
  { value: 'eu-west-1', label: 'Ireland', note: 'eu-west-1' },
  { value: 'eu-central-1', label: 'Frankfurt', note: 'eu-central-1' },
  { value: 'us-east-1', label: 'North Virginia', note: 'us-east-1' },
  { value: 'us-west-2', label: 'Oregon', note: 'us-west-2' },
  { value: 'ap-south-1', label: 'Mumbai', note: 'Not enabled', disabled: true },
  { value: 'ap-southeast-2', label: 'Sydney', note: 'ap-southeast-2' },
];

function Regions({ search = true }: { search?: boolean }) {
  const [picked, setPicked] = useState('eu-west-1');
  return (
    <div className="w-80 rounded-md border border-border bg-surface-elevated p-1">
      <Combobox label="Region" defaultValue={picked}>
        {search && <ComboboxInput placeholder="Search regions" />}
        {/* Without the input, the list takes focus from Tab. */}
        <ComboboxList label="Regions" {...(search ? {} : { tabIndex: 0 })}>
          {REGIONS.map((r) => (
            <ComboboxItem
              key={r.value}
              value={r.value}
              keywords={[r.label]}
              disabled={r.disabled}
              onSelect={() => setPicked(r.value)}
            >
              <Icon
                name={r.value === picked ? 'check' : 'globe'}
                size="2xs"
                color={r.value === picked ? 'accent' : 'subtle'}
                label=""
              />
              <Stack direction="horizontal" align="center" justify="between" grow>
                <Text as="span" size="supporting">
                  {r.label}
                </Text>
                <Text as="span" size="caption" color="subtle">
                  {r.note}
                </Text>
              </Stack>
            </ComboboxItem>
          ))}
          <ComboboxEmpty>No region matches.</ComboboxEmpty>
        </ComboboxList>
      </Combobox>
    </div>
  );
}

/** With a search field. Type "virginia" or "us-" to filter. */
export const Default: Story = { render: () => <Regions /> };

/** A short list without a search field. Tab to the list and use the arrow keys. */
export const WithoutSearch: Story = { render: () => <Regions search={false} /> };

/** Inside a popover, opened from a button. */
export const InPopover: Story = {
  render: () => (
    <Popover defaultOpen>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="sm">
          Region
          <Icon name="chevron-down" size="2xs" color="subtle" label="" />
        </Button>
      </PopoverTrigger>
      <PopoverContent wide>
        <Combobox label="Region">
          <ComboboxInput placeholder="Search regions" />
          <ComboboxList label="Regions">
            {REGIONS.map((r) => (
              <ComboboxItem
                key={r.value}
                value={r.value}
                keywords={[r.label]}
                disabled={r.disabled}
              >
                {r.label}
              </ComboboxItem>
            ))}
            <ComboboxEmpty>No region matches.</ComboboxEmpty>
          </ComboboxList>
        </Combobox>
      </PopoverContent>
    </Popover>
  ),
};
