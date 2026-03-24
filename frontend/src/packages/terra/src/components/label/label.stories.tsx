import type { Meta, StoryObj } from '@storybook/react';
import { Label } from './label';
import { Input } from '../input/input';

const meta: Meta<typeof Label> = {
  title: 'Terra/Atoms/Label',
  component: Label,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Label>;

export const Default: Story = {
  args: { children: 'Agent name' },
};

export const PairedWithInput: Story = {
  render: () => (
    <div className="flex flex-col gap-1.5 w-72">
      <Label htmlFor="paired-input">Policy name</Label>
      <Input id="paired-input" placeholder="e.g. read-only-s3" />
    </div>
  ),
};

export const PairedWithDisabledInput: Story = {
  render: () => (
    <div className="flex flex-col gap-1.5 w-72">
      <Label htmlFor="disabled-input">Service token</Label>
      <Input id="disabled-input" disabled placeholder="Not available" />
    </div>
  ),
};

export const MultipleLabels: Story = {
  render: () => (
    <div className="flex flex-col gap-4 w-72">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="ml-name">Name</Label>
        <Input id="ml-name" placeholder="Agent display name" />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="ml-id">Identifier</Label>
        <Input id="ml-id" placeholder="e.g. apollo-3" />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="ml-token">API token</Label>
        <Input id="ml-token" type="password" placeholder="sk-…" />
      </div>
    </div>
  ),
};
