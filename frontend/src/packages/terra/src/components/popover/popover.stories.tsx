import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Settings, Filter, Info, ChevronDown } from 'lucide-react';
import { Popover, PopoverTrigger, PopoverContent, PopoverAnchor } from './popover';
import { Button } from '../button/button';
import { Text } from '../typography/typography';
import { Stack } from '../layout/stack';

const meta: Meta = {
  title: 'Terra/Atoms/Popover',
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj;

export const Default: Story = {
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="secondary" size="sm">
          Open popover
        </Button>
      </PopoverTrigger>
      <PopoverContent className="p-3">
        <Stack gap="tight">
          <Text size="supporting" color="foreground">Popover content goes here.</Text>
          <Text size="micro" color="muted">Any React node is valid content.</Text>
        </Stack>
      </PopoverContent>
    </Popover>
  ),
};

function PolicyFilterDemo() {
  const [selected, setSelected] = React.useState<string[]>(['allow']);

  const toggle = (value: string) =>
    setSelected((prev) =>
      prev.includes(value) ? prev.filter((v) => v !== value) : [...prev, value],
    );

  const options = [
    { value: 'allow', label: 'Allow' },
    { value: 'deny', label: 'Deny' },
    { value: 'audit', label: 'Audit' },
  ];

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="secondary" size="sm" className="gap-1.5">
          <Filter size={13} />
          Filter
          {selected.length > 0 && (
            <output className="ml-0.5 rounded-full bg-accent/20 px-1.5 py-0.5 text-[10px] font-medium text-accent">
              {selected.length}
            </output>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-44 p-1.5">
        <div className="px-2 pb-1.5 pt-0.5">
          <Text size="micro" color="muted" weight="emphasis" transform="uppercase">Decision type</Text>
        </div>
        {options.map(({ value, label }) => (
          <button
            key={value}
            onClick={() => toggle(value)}
            className="flex w-full items-center gap-2.5 rounded px-2 py-1.5 text-sm text-foreground hover:bg-surface transition-colors"
          >
            <div
              className={`flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-sm border transition-colors ${
                selected.includes(value) ? 'border-accent bg-accent text-white' : 'border-border'
              }`}
            >
              {selected.includes(value) && (
                <svg viewBox="0 0 8 8" className="h-2 w-2 fill-current">
                  <path
                    d="M1 4l2.5 2.5L7 1.5"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    fill="none"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
              )}
            </div>
            {label}
          </button>
        ))}
      </PopoverContent>
    </Popover>
  );
}

export const PolicyFilter: Story = {
  name: 'Policy filter',
  render: () => <PolicyFilterDemo />,
};

export const AgentSettings: Story = {
  name: 'Agent settings',
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <button
          className="p-2 rounded-md text-muted hover:text-foreground hover:bg-surface-elevated transition-colors"
          aria-label="Agent settings"
        >
          <Settings size={15} />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-52 p-1.5">
        {[
          { label: 'View details', danger: false },
          { label: 'Edit config', danger: false },
          { label: 'Restart agent', danger: false },
          { label: 'Revoke access', danger: true },
        ].map(({ label, danger }) => (
          <button
            key={label}
            className={`w-full text-left rounded px-3 py-1.5 text-sm transition-colors ${
              danger
                ? 'text-status-error hover:bg-status-error/10'
                : 'text-foreground hover:bg-surface'
            }`}
          >
            {label}
          </button>
        ))}
      </PopoverContent>
    </Popover>
  ),
};

export const CredentialInfo: Story = {
  name: 'Credential info',
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <button
          className="flex items-center gap-1 text-xs text-muted hover:text-foreground transition-colors"
          aria-label="Credential details"
        >
          prod-s3-read <Info size={12} />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-64 p-3">
        <div className="mb-2">
          <Text size="micro" color="foreground" weight="emphasis">prod-s3-read</Text>
        </div>
        <dl className="space-y-1.5">
          {[
            { term: 'Type', detail: 'AWS IAM' },
            { term: 'Scope', detail: 's3:GetObject, s3:ListBucket' },
            { term: 'Region', detail: 'us-east-1' },
            { term: 'Expires', detail: 'in 2 days' },
          ].map(({ term, detail }) => (
            <div key={term} className="flex justify-between gap-3 text-xs">
              <dt className="text-muted">{term}</dt>
              <dd className="font-mono text-foreground">{detail}</dd>
            </div>
          ))}
        </dl>
      </PopoverContent>
    </Popover>
  ),
};

export const Positions: Story = {
  render: () => (
    <div className="grid grid-cols-2 gap-6 p-12">
      {(['top', 'right', 'bottom', 'left'] as const).map((side) => (
        <Popover key={side}>
          <PopoverTrigger asChild>
            <Button variant="secondary" size="sm" className="capitalize w-24 justify-between gap-1">
              {side} <ChevronDown size={12} />
            </Button>
          </PopoverTrigger>
          <PopoverContent side={side} className="px-3 py-2">
            <Stack direction="horizontal" gap="tight" align="center">
              <Text size="micro" color="muted">Opens on</Text>
              <Text size="micro" color="foreground">{side}</Text>
            </Stack>
          </PopoverContent>
        </Popover>
      ))}
    </div>
  ),
};

export const WithAnchor: Story = {
  name: 'Custom anchor',
  render: () => (
    <div className="flex items-center gap-2">
      <Text size="supporting" color="muted">Model:</Text>
      <Popover>
        <PopoverAnchor asChild>
          <code className="text-sm font-mono text-foreground">claude-sonnet-4</code>
        </PopoverAnchor>
        <PopoverTrigger asChild>
          <button
            className="text-muted hover:text-foreground transition-colors"
            aria-label="Model info"
          >
            <Info size={13} />
          </button>
        </PopoverTrigger>
        <PopoverContent className="w-56 p-3">
          <div className="mb-1">
            <Text size="micro" color="foreground" weight="emphasis">claude-sonnet-4</Text>
          </div>
          <Text size="micro" color="muted">200k context · $3 / 1M input tokens</Text>
        </PopoverContent>
      </Popover>
    </div>
  ),
};
