import type { Meta, StoryObj } from '@storybook/react';
import { Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { PageSectionHeader } from './page-section-header';

/**
 * PageSectionHeader
 *
 * The heading row at the top of a list or settings page. It has a title, an optional
 * description, and an optional button on the right.
 */
const meta: Meta<typeof PageSectionHeader> = {
  title: 'Terra/Molecules/PageSectionHeader',
  component: PageSectionHeader,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof PageSectionHeader>;

const createButton = (
  <Button size="sm">
    <Icon name="plus" />
    Create key
  </Button>
);

export const Overview: Story = {
  render: () => (
    <div className="max-w-3xl">
      <PageSectionHeader
        title="API keys"
        description="Manage your API keys and see how they are used."
        action={createButton}
      />
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="title" use="The page heading. Required.">
          <PageSectionHeader title="Overview" />
        </Option>
        <Option name="description" use="A short line of muted text under the title.">
          <PageSectionHeader
            title="Grants"
            description="Permissions given to people and agents in your workspace."
          />
        </Option>
        <Option name="action" use="A button on the right, usually the main action for the page.">
          <PageSectionHeader title="Grants" action={createButton} />
        </Option>
      </OptionTable>
    </div>
  ),
};
