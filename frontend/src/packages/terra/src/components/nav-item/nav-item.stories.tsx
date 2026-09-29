import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { NavItem } from './nav-item';

/**
 * NavItem
 *
 * One entry in a sidebar menu, with an optional icon and count. Use `asChild` to render a link
 * instead of a button.
 */
const meta: Meta<typeof NavItem> = {
  title: 'Terra/Atoms/NavItem',
  component: NavItem,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof NavItem>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Sidebar menu">
        <Stack gap="tight" className="w-56 rounded-lg bg-surface p-2">
          <NavItem state="active" icon={<Icon name="cpu" />} asChild>
            <a href="#">Agents</a>
          </NavItem>
          <NavItem icon={<Icon name="key" />} asChild>
            <a href="#">Credentials</a>
          </NavItem>
          <NavItem icon={<Icon name="shield" />} badge={2} asChild>
            <a href="#">Policies</a>
          </NavItem>
          <NavItem icon={<Icon name="activity" />} asChild>
            <a href="#">Events</a>
          </NavItem>
          <NavItem state="disabled" icon={<Icon name="settings" />} asChild>
            <a href="#">Settings</a>
          </NavItem>
        </Stack>
      </Example>
      <Example title="Compact list">
        <Stack gap="none" className="w-56 rounded-lg bg-surface p-2">
          {['Policy review: apollo-3', 'Credential audit', 'Incident 42 response'].map((label) => (
            <NavItem key={label} size="sm" icon={<Icon name="bot" size="2xs" />} asChild>
              <a href="#">{label}</a>
            </NavItem>
          ))}
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='state="default"' use="An entry that is not the current page.">
          <div className="w-56">
            <NavItem icon={<Icon name="cpu" />} asChild>
              <a href="#">Agents</a>
            </NavItem>
          </div>
        </Option>
        <Option name='state="active"' use="The page you are on. A bar on the left marks it.">
          <div className="w-56">
            <NavItem state="active" icon={<Icon name="cpu" />} asChild>
              <a href="#">Agents</a>
            </NavItem>
          </div>
        </Option>
        <Option
          name='state="disabled"'
          use="An entry that cannot be used. It does not react to clicks."
        >
          <div className="w-56">
            <NavItem state="disabled" icon={<Icon name="key" />} asChild>
              <a href="#">Credentials</a>
            </NavItem>
          </div>
        </Option>
        <Option name='size="md"' use="Standard menu entry. The default.">
          <div className="w-56">
            <NavItem icon={<Icon name="bot" />} asChild>
              <a href="#">Policy review</a>
            </NavItem>
          </div>
        </Option>
        <Option name='size="sm"' use="Tighter entry for long lists such as history.">
          <div className="w-56">
            <NavItem size="sm" icon={<Icon name="bot" size="2xs" />} asChild>
              <a href="#">Policy review</a>
            </NavItem>
          </div>
        </Option>
        <Option name="badge" use="A short count after the label.">
          <div className="w-56">
            <NavItem icon={<Icon name="triangle-alert" />} badge={4} asChild>
              <a href="#">Alerts</a>
            </NavItem>
          </div>
        </Option>
        <Option name="icon (omitted)" use="Text only, with no icon.">
          <div className="w-56">
            <NavItem asChild>
              <a href="#">Documentation</a>
            </NavItem>
          </div>
        </Option>
        <Option name="asChild" use="Style your own link so the entry navigates.">
          <div className="w-56">
            <NavItem icon={<Icon name="file-text" />} asChild>
              <a href="#docs">Documentation</a>
            </NavItem>
          </div>
        </Option>
      </OptionTable>
    </div>
  ),
};
