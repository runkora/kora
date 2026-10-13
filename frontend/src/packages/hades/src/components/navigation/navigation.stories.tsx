import type { Meta, StoryObj } from '@storybook/react';
import { NavItem } from '../nav-item/nav-item';
import { Stack } from '../layout/stack';
import { Navigation } from './navigation';

/**
 * Navigation
 *
 * A navigation landmark for a group of links, such as the main sidebar. It
 * exists so product code does not need a raw nav element.
 */
const meta: Meta<typeof Navigation> = {
  title: 'Hades/Layout/Navigation',
  component: Navigation,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof Navigation>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xs">
      <Navigation>
        <Stack gap="tight">
          <NavItem state="active" asChild>
            <a href="#">Overview</a>
          </NavItem>
          <NavItem asChild>
            <a href="#">Models</a>
          </NavItem>
          <NavItem asChild>
            <a href="#">API keys</a>
          </NavItem>
        </Stack>
      </Navigation>
    </div>
  ),
};
