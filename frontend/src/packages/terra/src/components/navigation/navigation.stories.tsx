import type { Meta, StoryObj } from '@storybook/react';
import { NavItem } from '../nav-item/nav-item';
import { Stack } from '../layout/stack';
import { Navigation } from './navigation';

const meta: Meta<typeof Navigation> = {
  title: 'Terra/Layout/Navigation',
  component: Navigation,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof Navigation>;

export const Default: Story = {
  render: () => (
    <Navigation>
      <Stack gap="tight">
        <NavItem state="active" asChild>
          <a href="#">Overview</a>
        </NavItem>
        <NavItem asChild>
          <a href="#">Models</a>
        </NavItem>
        <NavItem asChild>
          <a href="#">API Keys</a>
        </NavItem>
      </Stack>
    </Navigation>
  ),
};
