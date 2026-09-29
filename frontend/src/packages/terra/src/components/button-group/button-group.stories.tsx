import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { ButtonGroup } from './button-group';

/**
 * ButtonGroup
 *
 * Joins buttons into one connected row, such as a switch between two views.
 */
const meta: Meta<typeof ButtonGroup> = {
  title: 'Terra/Atoms/ButtonGroup',
  component: ButtonGroup,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof ButtonGroup>;

function ViewSwitch() {
  const [mode, setMode] = useState<'cards' | 'list'>('cards');
  return (
    <ButtonGroup role="group" aria-label="View mode">
      <Button
        variant={mode === 'cards' ? 'default' : 'outline'}
        size="icon-sm"
        aria-label="Cards"
        aria-pressed={mode === 'cards'}
        onClick={() => setMode('cards')}
      >
        <Icon name="layout-grid" size="2xs" />
      </Button>
      <Button
        variant={mode === 'list' ? 'default' : 'outline'}
        size="icon-sm"
        aria-label="List"
        aria-pressed={mode === 'list'}
        onClick={() => setMode('list')}
      >
        <Icon name="list" size="2xs" />
      </Button>
    </ButtonGroup>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="View switch">
        <ViewSwitch />
      </Example>
      <Example title="Text buttons">
        <ButtonGroup role="group" aria-label="Time range">
          <Button variant="default" size="sm">
            Day
          </Button>
          <Button variant="outline" size="sm">
            Week
          </Button>
          <Button variant="outline" size="sm">
            Month
          </Button>
        </ButtonGroup>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="ButtonGroup has no visual props of its own. It works with any Button variant and size."
      >
        <Option
          name='role="group"'
          use="Tell screen readers the buttons belong together. Add an aria-label to name the group."
        >
          <ButtonGroup role="group" aria-label="Alignment">
            <Button variant="outline" size="sm">
              Left
            </Button>
            <Button variant="outline" size="sm">
              Right
            </Button>
          </ButtonGroup>
        </Option>
        <Option
          name='variant="default" and "outline"'
          use="Show the chosen button filled and the rest outlined."
        >
          <ButtonGroup role="group" aria-label="Environment">
            <Button variant="outline" size="sm">
              Staging
            </Button>
            <Button variant="default" size="sm">
              Production
            </Button>
          </ButtonGroup>
        </Option>
        <Option name="Three or more buttons" use="Only the two outer buttons keep rounded corners.">
          <ButtonGroup role="group" aria-label="Sort by">
            <Button variant="outline" size="sm">
              Name
            </Button>
            <Button variant="outline" size="sm">
              Status
            </Button>
            <Button variant="outline" size="sm">
              Created
            </Button>
          </ButtonGroup>
        </Option>
        <Option name="asChild buttons" use="Links styled as buttons join the row the same way.">
          <ButtonGroup role="group" aria-label="Pages">
            <Button asChild variant="outline" size="sm">
              <a href="#agents">Agents</a>
            </Button>
            <Button asChild variant="outline" size="sm">
              <a href="#runs">Runs</a>
            </Button>
          </ButtonGroup>
        </Option>
      </OptionTable>
    </div>
  ),
};
