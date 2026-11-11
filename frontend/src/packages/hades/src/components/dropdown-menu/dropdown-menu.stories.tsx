import type { Meta, StoryObj } from '@storybook/react';
import { Example, Frame, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Icon } from '../icon';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from './dropdown-menu';

/**
 * A short list of actions that opens from a button, such as a "…" menu.
 * Arrow keys move between items and Escape closes it. The samples here are
 * held open so you can see them.
 */
const meta: Meta<typeof DropdownMenu> = {
  title: 'Hades/Molecules/DropdownMenu',
  component: DropdownMenu,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof DropdownMenu>;

function Menu({ danger }: { danger?: boolean }) {
  return (
    <Frame className="flex min-h-48 items-start justify-center">
      <DropdownMenu open modal={false}>
        <DropdownMenuTrigger asChild>
          <Button size="icon-sm" variant="ghost" aria-label="More actions">
            <Icon name="more-horizontal" size="2xs" label="" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem>
            <Icon name="activity" size="2xs" label="" />
            Open trace
          </DropdownMenuItem>
          <DropdownMenuItem>
            <Icon name="copy" size="2xs" label="" />
            Copy run ID
          </DropdownMenuItem>
          {danger && (
            <DropdownMenuItem tone="danger">
              <Icon name="trash" size="2xs" label="" />
              Delete run
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </Frame>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl">
      <Example title="Overflow menu">
        <Menu />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-3xl">
      <OptionTable title="Options" note="Each item can render a link with asChild.">
        <Option name='DropdownMenuItem tone="danger"' use="For an action that removes something.">
          <Menu danger />
        </Option>
      </OptionTable>
    </div>
  ),
};
