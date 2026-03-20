import type { Meta, StoryObj } from '@storybook/react';
import { Input } from './input';

const meta: Meta<typeof Input> = {
  title: 'Terra/Atoms/Input',
  component: Input,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
  argTypes: {
    type: {
      control: 'select',
      options: ['text', 'email', 'password', 'number', 'search', 'url'],
    },
    hasError: { control: 'boolean' },
    disabled: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof Input>;

export const Default: Story = {
  args: { placeholder: 'Enter a value…', className: 'w-72' },
};

export const WithValue: Story = {
  args: { defaultValue: 'apollo-3', className: 'w-72' },
};

export const Email: Story = {
  args: { type: 'email', placeholder: 'you@example.com', className: 'w-72' },
};

export const Password: Story = {
  args: { type: 'password', placeholder: 'Enter password', className: 'w-72' },
};

export const ErrorState: Story = {
  args: { hasError: true, defaultValue: 'bad-value', className: 'w-72' },
};

export const Disabled: Story = {
  args: { disabled: true, placeholder: 'Not editable', className: 'w-72' },
};

export const DisabledWithValue: Story = {
  args: { disabled: true, defaultValue: 'read-only-value', className: 'w-72' },
};

export const AllStates: Story = {
  render: () => (
    <div className="flex flex-col gap-3 w-72">
      <Input placeholder="Default" />
      <Input defaultValue="With value" />
      <Input hasError defaultValue="Error state" />
      <Input disabled placeholder="Disabled" />
      <Input disabled defaultValue="Disabled with value" />
    </div>
  ),
};
