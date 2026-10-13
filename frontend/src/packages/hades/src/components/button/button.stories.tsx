import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Button } from './button';

/**
 * Button
 *
 * The clickable control for actions. Pick a variant for how strong the action
 * is, and a size for where it sits.
 */
const meta: Meta<typeof Button> = {
  title: 'Hades/Atoms/Button',
  component: Button,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Button>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Main action and a way out">
        <Stack direction="horizontal" gap="snug">
          <Button>Deploy agent</Button>
          <Button variant="secondary">Cancel</Button>
        </Stack>
      </Example>
      <Example title="With an icon">
        <Stack direction="horizontal" gap="snug">
          <Button>
            <Icon name="rocket" size="sm" />
            Run agents
          </Button>
          <Button variant="outline">
            <Icon name="plus" size="sm" />
            New agent
          </Button>
        </Stack>
      </Example>
      <Example title="Icon only">
        <Stack direction="horizontal" gap="snug">
          <Button size="icon" variant="ghost" aria-label="Copy">
            <Icon name="copy" size="sm" />
          </Button>
          <Button size="icon-sm" variant="ghost" aria-label="Settings">
            <Icon name="settings" size="sm" />
          </Button>
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl space-y-10">
      <OptionTable title="Variant" note="How strong the action looks.">
        <Option name='variant="default"' use="The main action on a screen. The default.">
          <Button>Deploy agent</Button>
        </Option>
        <Option name='variant="secondary"' use="A second action next to the main one.">
          <Button variant="secondary">Cancel</Button>
        </Option>
        <Option name='variant="outline"' use="An action that should not stand out. Has a border.">
          <Button variant="outline">View logs</Button>
        </Option>
        <Option name='variant="ghost"' use="A light action with no border, for toolbars and rows.">
          <Button variant="ghost">Edit</Button>
        </Option>
        <Option
          name='variant="muted"'
          use="Fades into the background until hovered. For small extras like Clear."
        >
          <Button variant="muted">Clear</Button>
        </Option>
        <Option name='variant="outline-muted"' use="A filter or toggle that is switched off.">
          <Button variant="outline-muted">Failed</Button>
        </Option>
        <Option name='variant="accent-outline"' use="A filter or toggle that is switched on.">
          <Button variant="accent-outline">Running</Button>
        </Option>
        <Option name='variant="destructive"' use="An action that deletes or removes something.">
          <Button variant="destructive">
            <Icon name="trash" size="sm" />
            Delete agent
          </Button>
        </Option>
      </OptionTable>

      <OptionTable title="Size">
        <Option name='size="xs"' use="Very compact, such as chips and send buttons.">
          <Button size="xs">Send</Button>
        </Option>
        <Option name='size="sm"' use="Toolbars and dense areas.">
          <Button size="sm">Save</Button>
        </Option>
        <Option name='size="md"' use="Most buttons and form submits. The default.">
          <Button size="md">Save</Button>
        </Option>
        <Option name='size="lg"' use="Prominent actions, such as in a dialog.">
          <Button size="lg">Continue</Button>
        </Option>
        <Option name='size="xl"' use="Large touch target for mobile screens.">
          <Button size="xl">Continue</Button>
        </Option>
        <Option name='size="icon"' use="Square button holding only an icon. Needs an aria-label.">
          <Button size="icon" variant="outline" aria-label="Copy">
            <Icon name="copy" size="sm" />
          </Button>
        </Option>
        <Option name='size="icon-sm"' use="Smaller square icon button. Needs an aria-label.">
          <Button size="icon-sm" variant="outline" aria-label="Copy">
            <Icon name="copy" size="sm" />
          </Button>
        </Option>
        <Option
          name='size="tile"'
          use="A card that acts as one big button. Height follows the content."
        >
          <div className="w-64">
            <Button size="tile" variant="outline" fullWidth>
              <Stack gap="tight" padding="cozy">
                <Icon name="bot" size="md" />
                <span>Chat agent</span>
              </Stack>
            </Button>
          </div>
        </Option>
        <Option
          name='responsiveSize="xl"'
          use="Use a different size below the large breakpoint. Here: xl on small screens, md on desktop."
        >
          <Button size="md" responsiveSize="xl">
            Continue
          </Button>
        </Option>
      </OptionTable>

      <OptionTable title="Shape and state">
        <Option name='shape="pill"' use="Fully rounded ends, for chips and send buttons.">
          <Button size="xs" shape="pill">
            <Icon name="send" size="sm" />
            Send
          </Button>
        </Option>
        <Option name='shape="square"' use="No rounded corners, for rows inside a divided card.">
          <Button variant="outline" shape="square">
            Row action
          </Button>
        </Option>
        <Option name="disabled" use="The action is not available right now.">
          <Button disabled>Deploy agent</Button>
        </Option>
        <Option name="selected" use="Marks the chosen row in a list.">
          <Button variant="ghost" selected>
            production-agent
          </Button>
        </Option>
        <Option name="dim" use="Fades until hovered, for a toggle that is off.">
          <Button variant="ghost" size="icon-sm" dim aria-label="Favourite">
            <Icon name="heart" size="sm" />
          </Button>
        </Option>
        <Option name="fullWidth" use="Stretch to fill the space it sits in.">
          <div className="w-64">
            <Button variant="outline" fullWidth>
              <Icon name="plus" size="sm" />
              New chat
            </Button>
          </div>
        </Option>
        <Option name="asChild" use="Style your own element, such as a link, as a button.">
          <Button asChild variant="outline">
            <a href="#agents">View agents</a>
          </Button>
        </Option>
      </OptionTable>
    </div>
  ),
};
