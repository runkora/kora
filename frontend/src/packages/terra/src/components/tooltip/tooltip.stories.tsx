import type { Meta, StoryObj } from '@storybook/react';
import { Info, Trash2, Copy } from 'lucide-react';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './tooltip';

const meta: Meta = {
  title: 'Terra/Atoms/Tooltip',
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
  decorators: [
    (Story) => (
      <TooltipProvider>
        <Story />
      </TooltipProvider>
    ),
  ],
};

export default meta;
type Story = StoryObj;

export const Default: Story = {
  render: () => (
    <Tooltip>
      <TooltipTrigger className="text-sm text-muted underline decoration-dotted cursor-help">
        Hover me
      </TooltipTrigger>
      <TooltipContent>Supplementary context</TooltipContent>
    </Tooltip>
  ),
};

export const OnIconButton: Story = {
  render: () => (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          className="p-2 rounded-md text-muted hover:text-foreground hover:bg-surface-elevated transition-colors"
          aria-label="View policy info"
        >
          <Info size={16} />
        </button>
      </TooltipTrigger>
      <TooltipContent>View policy details</TooltipContent>
    </Tooltip>
  ),
};

export const DestructiveAction: Story = {
  render: () => (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          className="p-2 rounded-md text-muted hover:text-status-error hover:bg-surface-elevated transition-colors"
          aria-label="Revoke credentials"
        >
          <Trash2 size={16} />
        </button>
      </TooltipTrigger>
      <TooltipContent>Revoke credentials — this cannot be undone</TooltipContent>
    </Tooltip>
  ),
};

export const Positions: Story = {
  render: () => (
    <div className="grid grid-cols-2 gap-8 p-12">
      {(['top', 'right', 'bottom', 'left'] as const).map((side) => (
        <Tooltip key={side}>
          <TooltipTrigger className="px-3 py-1.5 text-xs rounded border border-border text-muted capitalize">
            {side}
          </TooltipTrigger>
          <TooltipContent side={side}>Tooltip on {side}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  ),
};

export const CopyAction: Story = {
  render: () => (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          className="flex items-center gap-1.5 px-2 py-1 rounded text-xs font-mono text-muted bg-surface-elevated border border-border-subtle hover:text-foreground transition-colors"
          aria-label="Copy agent ID"
        >
          apollo-3 <Copy size={12} />
        </button>
      </TooltipTrigger>
      <TooltipContent>Copy agent ID to clipboard</TooltipContent>
    </Tooltip>
  ),
};
