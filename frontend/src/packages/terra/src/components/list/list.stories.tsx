import type { Meta, StoryObj } from '@storybook/react';
import { Example, Frame, Option, OptionTable } from '../../docs/story-ui';
import { Text } from '../typography/typography';
import { List, ListItem } from './list';

/**
 * List
 *
 * A list of related items, numbered when order matters. Items are stacked
 * with a set gap and can have a line between them.
 */
const meta: Meta<typeof List> = {
  title: 'Terra/Atoms/List',
  component: List,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof List>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="Default">
        <List gap="snug">
          <ListItem>
            <Text>Agents</Text>
          </ListItem>
          <ListItem>
            <Text>Policies</Text>
          </ListItem>
          <ListItem>
            <Text>Connections</Text>
          </ListItem>
        </List>
      </Example>
      <Example title="Numbered steps">
        <List ordered gap="snug">
          <ListItem>
            <Text>1. Create the agent</Text>
          </ListItem>
          <ListItem>
            <Text>2. Grant it access</Text>
          </ListItem>
          <ListItem>
            <Text>3. Deploy agent</Text>
          </ListItem>
        </List>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="A list adds no bullets or numbers of its own. Put the marker inside each item if you want one."
      >
        <Option
          name="ordered"
          use="Uses a numbered list element so screen readers announce the order."
        >
          <Frame>
            <List ordered gap="tight">
              <ListItem>
                <Text>Create the agent</Text>
              </ListItem>
              <ListItem>
                <Text>Deploy agent</Text>
              </ListItem>
            </List>
          </Frame>
        </Option>
        <Option
          name='gap="none" / "comfortable"'
          use="Space between items. The names are on the Spacing page."
        >
          <Frame>
            <List gap="comfortable">
              <ListItem>
                <Text>Agents</Text>
              </ListItem>
              <ListItem>
                <Text>Policies</Text>
              </ListItem>
            </List>
          </Frame>
        </Option>
        <Option name="separator" use="Draws a thin line between items.">
          <Frame>
            <List separator gap="snug">
              <ListItem>
                <Text>Agents</Text>
              </ListItem>
              <ListItem>
                <Text>Policies</Text>
              </ListItem>
              <ListItem>
                <Text>Connections</Text>
              </ListItem>
            </List>
          </Frame>
        </Option>
      </OptionTable>
    </div>
  ),
};
