import type { Meta, StoryObj } from '@storybook/react';
import { Rocket, Trash2, Settings } from 'lucide-react';
import { Button } from './button';

const meta: Meta<typeof Button> = {
  title: 'Terra/Atoms/Button',
  component: Button,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
  argTypes: {
    variant: {
      control: 'select',
      options: ['default', 'secondary', 'outline', 'ghost', 'destructive'],
    },
    size: {
      control: 'select',
      options: ['sm', 'md', 'lg', 'icon'],
    },
    disabled: { control: 'boolean' },
    asChild: { table: { disable: true } },
  },
};

export default meta;
type Story = StoryObj<typeof Button>;

export const Default: Story = {
  args: { children: 'Deploy agent', variant: 'default' },
};

export const Secondary: Story = {
  args: { children: 'View details', variant: 'secondary' },
};

export const Outline: Story = {
  args: { children: 'Configure', variant: 'outline' },
};

export const Ghost: Story = {
  args: { children: 'Dismiss', variant: 'ghost' },
};

export const Destructive: Story = {
  args: { children: 'Revoke access', variant: 'destructive' },
};

export const WithIcon: Story = {
  args: {
    children: (
      <>
        <Rocket /> Launch
      </>
    ),
    variant: 'default',
  },
};

export const IconOnly: Story = {
  args: {
    children: <Settings />,
    variant: 'ghost',
    size: 'icon',
    'aria-label': 'Settings',
  },
};

export const Disabled: Story = {
  args: { children: 'Unavailable', disabled: true },
};

export const AllVariants: Story = {
  render: () => (
    <div className="flex flex-wrap gap-3 p-4">
      <Button variant="default">Default</Button>
      <Button variant="secondary">Secondary</Button>
      <Button variant="outline">Outline</Button>
      <Button variant="ghost">Ghost</Button>
      <Button variant="destructive">
        <Trash2 /> Destructive
      </Button>
    </div>
  ),
};

export const AllSizes: Story = {
  render: () => (
    <div className="flex items-center gap-3 p-4">
      <Button size="sm">Small</Button>
      <Button size="md">Medium</Button>
      <Button size="lg">Large</Button>
      <Button size="icon" aria-label="Settings">
        <Settings />
      </Button>
    </div>
  ),
};
