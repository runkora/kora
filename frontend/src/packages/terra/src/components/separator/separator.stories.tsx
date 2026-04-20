import type { Meta, StoryObj } from '@storybook/react';
import { Separator } from './separator';
import { Text } from '../typography/typography';

const meta: Meta<typeof Separator> = {
  title: 'Terra/Atoms/Separator',
  component: Separator,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
  argTypes: {
    orientation: {
      control: 'select',
      options: ['horizontal', 'vertical'],
    },
    span: {
      control: 'select',
      options: ['full', 'wide', 'narrow', 'slim'],
    },
    decorative: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof Separator>;

export const Horizontal: Story = {
  render: () => (
    <div className="w-72">
      <Separator />
    </div>
  ),
};

export const Vertical: Story = {
  render: () => (
    <div className="flex items-center h-8 gap-4">
      <Text size="supporting" color="muted">Agents</Text>
      <Separator orientation="vertical" />
      <Text size="supporting" color="muted">Policies</Text>
      <Separator orientation="vertical" />
      <Text size="supporting" color="muted">Events</Text>
    </div>
  ),
};

export const SpanWide: Story = {
  render: () => (
    <div className="w-72 flex flex-col gap-4">
      <p className="text-xs text-muted text-center">80% of parent</p>
      <Separator span="wide" />
    </div>
  ),
};

export const SpanNarrow: Story = {
  render: () => (
    <div className="w-72 flex flex-col gap-4">
      <p className="text-xs text-muted text-center">40% of parent</p>
      <Separator span="narrow" />
    </div>
  ),
};

export const SpanSlim: Story = {
  render: () => (
    <div className="w-72 flex flex-col gap-4">
      <p className="text-xs text-muted text-center">20% of parent</p>
      <Separator span="slim" />
    </div>
  ),
};

export const AllSpans: Story = {
  render: () => (
    <div className="w-72 flex flex-col gap-6">
      <div className="flex flex-col gap-1.5">
        <p className="text-xs text-muted">full (100%)</p>
        <Separator span="full" />
      </div>
      <div className="flex flex-col gap-1.5">
        <p className="text-xs text-muted">wide (80%)</p>
        <Separator span="wide" />
      </div>
      <div className="flex flex-col gap-1.5">
        <p className="text-xs text-muted">narrow (40%)</p>
        <Separator span="narrow" />
      </div>
      <div className="flex flex-col gap-1.5">
        <p className="text-xs text-muted">slim (20%)</p>
        <Separator span="slim" />
      </div>
    </div>
  ),
};

export const BetweenSections: Story = {
  render: () => (
    <div className="flex flex-col gap-4 w-72">
      <div>
        <p className="text-sm font-medium text-foreground">Apollo agent</p>
        <p className="text-xs text-muted mt-1">Running · 3 tools active</p>
      </div>
      <Separator />
      <div>
        <p className="text-sm font-medium text-foreground">Hermes agent</p>
        <p className="text-xs text-muted mt-1">Idle · 0 tools active</p>
      </div>
      <Separator />
      <div>
        <p className="text-sm font-medium text-foreground">Minerva agent</p>
        <p className="text-xs text-muted mt-1">Suspended · 1 tool active</p>
      </div>
    </div>
  ),
};

export const InToolbar: Story = {
  render: () => (
    <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-surface w-fit">
      <button className="text-xs text-muted hover:text-foreground px-2 py-1 rounded">Bold</button>
      <button className="text-xs text-muted hover:text-foreground px-2 py-1 rounded">Italic</button>
      <Separator orientation="vertical" />
      <button className="text-xs text-muted hover:text-foreground px-2 py-1 rounded">Code</button>
      <button className="text-xs text-muted hover:text-foreground px-2 py-1 rounded">Link</button>
      <Separator orientation="vertical" />
      <button className="text-xs text-muted hover:text-foreground px-2 py-1 rounded">Clear</button>
    </div>
  ),
};
