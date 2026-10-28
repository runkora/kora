import type { Meta, StoryObj } from '@storybook/react';
import { Popover, PopoverContent, PopoverTrigger } from '../popover';
import { InlineToken } from './inline-token';

/**
 * InlineToken
 *
 * A button that reads as a word in a sentence. No fill at rest; a soft fill
 * on hover, focus, or while the picker it opens is showing.
 */
const meta: Meta<typeof InlineToken> = {
  title: 'Hades/Atoms/InlineToken',
  component: InlineToken,
  tags: ['autodocs'],
  parameters: { layout: 'centered' },
  argTypes: {
    mono: { control: 'boolean' },
    chevron: { control: 'boolean' },
    tone: { control: 'select', options: ['foreground', 'subtle'] },
    disabled: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof InlineToken>;

export const Default: Story = { args: { children: 'pull_request', mono: true } };

export const Empty: Story = { args: { children: 'Choose a field', tone: 'subtle' } };

export const AsPopoverTrigger: Story = {
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <InlineToken mono>pull_request</InlineToken>
      </PopoverTrigger>
      <PopoverContent>Picker content</PopoverContent>
    </Popover>
  ),
};
