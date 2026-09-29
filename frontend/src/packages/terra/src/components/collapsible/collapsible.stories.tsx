import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from './index';

/**
 * Collapsible
 *
 * A section that opens and closes when you press its trigger. It has no
 * styling of its own, so you build the look around it.
 */
const meta: Meta<typeof Collapsible> = {
  title: 'Terra/Atoms/Collapsible',
  component: Collapsible,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Collapsible>;

function ToolList({ name, tools, open }: { name: string; tools: string[]; open?: boolean }) {
  return (
    <Collapsible {...(open === undefined ? {} : { defaultOpen: open })}>
      <Stack gap="snug" align="start">
        <CollapsibleTrigger asChild>
          <Button variant="ghost" size="sm">
            <Icon name="chevron-down" size="sm" />
            {name}: {tools.length} tools
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <Stack gap="tight">
            {tools.map((tool) => (
              <Text key={tool} size="supporting" color="muted" mono>
                {tool}
              </Text>
            ))}
          </Stack>
        </CollapsibleContent>
      </Stack>
    </Collapsible>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="Open at the start">
        <ToolList
          name="github-mcp"
          open
          tools={['create_issue', 'list_pull_requests', 'merge_pull_request', 'search_code']}
        />
      </Example>
      <Example title="Closed at the start">
        <ToolList name="slack-mcp" tools={['post_message', 'list_channels']} />
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="defaultOpen" use="Start open. Without it the section starts closed.">
          <ToolList name="github-mcp" open tools={['create_issue', 'search_code']} />
        </Option>
        <Option
          name="open and onOpenChange"
          use="Control the state yourself. Pass open and update it in onOpenChange."
        >
          <Text size="supporting" color="muted">
            No visual difference. Use it when other parts of the page need to know if it is open.
          </Text>
        </Option>
        <Option name="disabled" use="The trigger cannot open or close the section.">
          <Collapsible disabled>
            <CollapsibleTrigger asChild>
              <Button variant="ghost" size="sm" disabled>
                Locked section
              </Button>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <Text size="supporting">Hidden content</Text>
            </CollapsibleContent>
          </Collapsible>
        </Option>
        <Option
          name="CollapsibleTrigger asChild"
          use="Use your own button as the trigger instead of a plain one."
        >
          <Collapsible>
            <CollapsibleTrigger asChild>
              <Button variant="outline" size="sm">
                Show details
              </Button>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <Text size="supporting" color="muted">
                Created by dev@orbit.local
              </Text>
            </CollapsibleContent>
          </Collapsible>
        </Option>
      </OptionTable>
    </div>
  ),
};
