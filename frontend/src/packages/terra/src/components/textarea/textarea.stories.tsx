import type { Meta, StoryObj } from '@storybook/react';
import { Textarea } from './textarea';
import { Text } from '../typography/typography';

const meta: Meta<typeof Textarea> = {
  title: 'Terra/Atoms/Textarea',
  component: Textarea,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
  argTypes: {
    variant: {
      control: 'select',
      options: ['default', 'bare'],
    },
    hasError: { control: 'boolean' },
    disabled: { control: 'boolean' },
    autoResize: { control: 'boolean' },
    rows: { control: { type: 'number', min: 1, max: 20 } },
  },
};

export default meta;
type Story = StoryObj<typeof Textarea>;

export const Default: Story = {
  args: {
    placeholder: 'Enter a description…',
    rows: 4,
    className: 'w-80',
  },
};

export const WithValue: Story = {
  args: {
    defaultValue:
      'Monitors agent policy compliance and emits structured audit events to the event spine for downstream processing.',
    rows: 4,
    className: 'w-80',
  },
};

export const ErrorState: Story = {
  args: {
    hasError: true,
    defaultValue: 'This value is invalid.',
    rows: 4,
    className: 'w-80',
  },
};

export const Disabled: Story = {
  args: {
    disabled: true,
    placeholder: 'Not editable',
    rows: 4,
    className: 'w-80',
  },
};

export const BareVariant: Story = {
  render: () => (
    <div className="w-80 rounded-2xl border border-border focus-within:border-accent transition-colors bg-surface-elevated px-1">
      <Textarea variant="bare" placeholder="Type a message…" rows={3} />
    </div>
  ),
};

export const BareWithError: Story = {
  render: () => (
    <div className="w-80 rounded-2xl border border-status-error bg-surface-elevated px-1">
      <Textarea
        variant="bare"
        hasError
        defaultValue="Malformed input that the container signals as invalid."
        rows={3}
      />
    </div>
  ),
};

export const AutoResize: Story = {
  args: {
    placeholder: 'Start typing — this textarea grows as you type…',
    autoResize: true,
    maxRows: 8,
    className: 'w-80',
  },
};

export const ChatInput: Story = {
  render: () => (
    <div className="w-96 rounded-2xl border border-border focus-within:border-accent transition-colors bg-surface-elevated">
      <div className="px-1">
        <Textarea variant="bare" placeholder="Ask the agent anything…" autoResize maxRows={6} />
      </div>
      <div className="flex items-center justify-between px-3 py-2 border-t border-border-subtle">
        <Text size="micro" color="subtle">
          Shift+Enter for new line
        </Text>
        <button className="text-xs bg-accent text-white rounded-md px-3 py-1 font-medium">
          Send
        </button>
      </div>
    </div>
  ),
};

export const AllStates: Story = {
  render: () => (
    <div className="flex flex-col gap-4 w-80">
      <Textarea placeholder="Default" rows={3} />
      <Textarea defaultValue="With value" rows={3} />
      <Textarea hasError defaultValue="Error state" rows={3} />
      <Textarea disabled placeholder="Disabled" rows={3} />
    </div>
  ),
};
