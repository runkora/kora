import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { ButtonGroup } from './button-group';

const meta: Meta<typeof ButtonGroup> = {
  title: 'Terra/Atoms/ButtonGroup',
  component: ButtonGroup,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof ButtonGroup>;

/** Icon-button toggle — the canonical view-mode switcher */
export const Default: Story = {
  render: () => (
    <ButtonGroup>
      <Button variant="default" size="icon-sm" aria-label="Grid view">
        <Icon name="layout-grid" size="2xs" />
      </Button>
      <Button variant="outline" size="icon-sm" aria-label="List view">
        <Icon name="list" size="2xs" />
      </Button>
    </ButtonGroup>
  ),
};

/** Text buttons fused into a segmented control */
export const TextButtons: Story = {
  render: () => (
    <ButtonGroup>
      <Button variant="default" size="sm">
        Overview
      </Button>
      <Button variant="outline" size="sm">
        Agents
      </Button>
      <Button variant="outline" size="sm">
        Policies
      </Button>
    </ButtonGroup>
  ),
};

function InteractiveExample() {
  const [active, setActive] = useState<'cards' | 'list'>('cards');
  return (
    <Stack direction="vertical" gap="comfortable" align="center">
      <ButtonGroup>
        <Button
          variant={active === 'cards' ? 'default' : 'outline'}
          size="icon-sm"
          aria-label="Cards view"
          onClick={() => setActive('cards')}
        >
          <Icon name="layout-grid" size="2xs" />
        </Button>
        <Button
          variant={active === 'list' ? 'default' : 'outline'}
          size="icon-sm"
          aria-label="List view"
          onClick={() => setActive('list')}
        >
          <Icon name="list" size="2xs" />
        </Button>
      </ButtonGroup>
      <Text size="caption" color="muted">
        Active: {active}
      </Text>
    </Stack>
  );
}

/** Interactive — active tab highlights; click to switch the selection */
export const Interactive: Story = {
  render: () => <InteractiveExample />,
};
