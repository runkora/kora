import type { Meta, StoryObj } from '@storybook/react';
import { NavItem } from './nav-item';
import {
  CpuIcon,
  KeyIcon,
  ShieldIcon,
  ActivityIcon,
  SettingsIcon,
  MessageSquareIcon,
  AlertTriangleIcon,
} from 'lucide-react';

const meta: Meta<typeof NavItem> = {
  title: 'Terra/Molecules/NavItem',
  component: NavItem,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
  argTypes: {
    state: {
      control: 'select',
      options: ['default', 'active', 'disabled'],
    },
    size: {
      control: 'select',
      options: ['md', 'sm'],
    },
  },
};

export default meta;
type Story = StoryObj<typeof NavItem>;

export const Default: Story = {
  args: {
    state: 'default',
    icon: <CpuIcon />,
    children: 'Agents',
  },
};

export const Active: Story = {
  args: {
    state: 'active',
    icon: <CpuIcon />,
    children: 'Agents',
  },
};

export const Disabled: Story = {
  args: {
    state: 'disabled',
    icon: <KeyIcon />,
    children: 'Credentials',
  },
};

export const WithBadge: Story = {
  args: {
    state: 'default',
    icon: <AlertTriangleIcon />,
    badge: 4,
    children: 'Alerts',
  },
};

export const ActiveWithBadge: Story = {
  args: {
    state: 'active',
    icon: <ActivityIcon />,
    badge: 12,
    children: 'Events',
  },
};

export const TextOnly: Story = {
  args: {
    state: 'default',
    children: 'Documentation',
  },
};

export const Compact: Story = {
  args: {
    state: 'default',
    size: 'sm',
    icon: <MessageSquareIcon />,
    children: 'Policy review thread',
  },
};

export const PrimaryNavigation: Story = {
  render: () => (
    <div className="flex flex-col gap-1 w-56 p-2 bg-surface rounded-lg">
      <NavItem state="active" icon={<CpuIcon />}>
        Agents
      </NavItem>
      <NavItem state="default" icon={<KeyIcon />}>
        Credentials
      </NavItem>
      <NavItem state="default" icon={<ShieldIcon />} badge={2}>
        Policies
      </NavItem>
      <NavItem state="default" icon={<ActivityIcon />}>
        Events
      </NavItem>
      <NavItem state="disabled" icon={<SettingsIcon />}>
        Settings
      </NavItem>
    </div>
  ),
};

export const CompactList: Story = {
  render: () => (
    <div className="flex flex-col gap-0.5 w-56 p-2 bg-surface rounded-lg">
      {[
        'Policy review — apollo-3',
        'Credential audit thread',
        'Incident #42 response',
        'MCP tool registration',
        'Access scope discussion',
      ].map((label) => (
        <NavItem key={label} size="sm" icon={<MessageSquareIcon />} state="default">
          {label}
        </NavItem>
      ))}
    </div>
  ),
};

export const AllStates: Story = {
  render: () => (
    <div className="flex flex-col gap-1 w-56">
      <NavItem state="default" icon={<CpuIcon />}>
        Default
      </NavItem>
      <NavItem state="active" icon={<ShieldIcon />}>
        Active
      </NavItem>
      <NavItem state="disabled" icon={<KeyIcon />}>
        Disabled
      </NavItem>
    </div>
  ),
};
