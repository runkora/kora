import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Text } from '../typography/typography';
import { EmptyState } from './empty-state';

/**
 * A placeholder for a list or page that has nothing in it yet. It says what is missing and can
 * offer the next step.
 */
const meta: Meta<typeof EmptyState> = {
  title: 'Terra/Molecules/EmptyState',
  component: EmptyState,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof EmptyState>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="An empty list">
        <Text size="supporting" color="muted">
          Shown in place of a list that has no items, with a button to add the first one.
        </Text>
        <EmptyState
          icon="key"
          title="No API keys yet"
          description="Create a key to start making requests."
          action={<Button size="sm">Create key</Button>}
        />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options" note="Icon, title, and description are required.">
        <Option name="Without action" use="When there is nothing the user can do about it here.">
          <EmptyState
            icon="shield-check"
            title="No grants yet"
            description="Grants give a person or agent access."
          />
        </Option>
        <Option
          name="action"
          use="Offers the next step, usually a button that adds the first item."
        >
          <EmptyState
            icon="key"
            title="No API keys yet"
            description="Create a key to start making requests."
            action={<Button size="sm">Create key</Button>}
          />
        </Option>
        <Option name="Search with no results" use="When filters or a search hide every item.">
          <EmptyState
            icon="search"
            title="No matches"
            description="Nothing matches your filters. Try a different search."
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
