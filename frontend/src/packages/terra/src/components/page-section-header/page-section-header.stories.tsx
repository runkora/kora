import type { Meta, StoryObj } from '@storybook/react';
import { Button, Icon } from '../..';
import { PageSectionHeader } from './page-section-header';

const meta: Meta<typeof PageSectionHeader> = {
  title: 'Terra/Layout/PageSectionHeader',
  component: PageSectionHeader,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof PageSectionHeader>;

export const Default: Story = {
  args: {
    title: 'API Keys',
    description: 'Manage your API keys and monitor their usage.',
  },
};

export const WithAction: Story = {
  args: {
    title: 'Grants',
    description: 'Permission grants assigned to principals in your tenant.',
    action: (
      <Button size="sm">
        <Icon name="plus" />
        Create Grant
      </Button>
    ),
  },
};

export const TitleOnly: Story = {
  args: { title: 'Overview' },
};

export const ActionOnly: Story = {
  args: {
    title: 'Roles',
    action: (
      <Button size="sm" variant="outline">
        <Icon name="plus" />
        Create Role
      </Button>
    ),
  },
};
