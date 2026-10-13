import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Text } from '../typography/typography';
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from './card';

/**
 * Card
 *
 * A box that groups related content on a slightly raised surface. Build it from a header, a
 * body and a footer.
 */
const meta: Meta<typeof Card> = {
  title: 'Hades/Atoms/Card',
  component: Card,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Card>;

function Body({ children = 'Working through 14 pending tasks across 3 tools.' }) {
  return (
    <Text size="supporting" color="muted">
      {children}
    </Text>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Header, body and footer">
        <Card>
          <CardHeader>
            <CardTitle>Agent: apollo-3</CardTitle>
            <CardDescription>Last active 2 minutes ago</CardDescription>
          </CardHeader>
          <CardContent>
            <Body />
          </CardContent>
          <CardFooter justify="end">
            <Button size="sm" variant="ghost">
              View log
            </Button>
          </CardFooter>
        </Card>
      </Example>
      <Example title="Header shorthand">
        <Card>
          <CardHeader
            leadingIcon="user"
            title="Identity"
            description="Who this agent acts as"
            action={
              <Button size="sm" variant="ghost">
                Edit
              </Button>
            }
            divider
          />
          <CardContent padded>
            <Body>Acts for justin@example.com with read access to 3 tools.</Body>
          </CardContent>
        </Card>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl space-y-10">
      <OptionTable title="Card">
        <Option name="default" use="A raised surface with a soft border.">
          <Card>
            <CardContent padded>
              <Body />
            </CardContent>
          </Card>
        </Option>
        <Option name="subtle" use="A lighter surface, for a card inside a panel or dialog.">
          <Card subtle>
            <CardContent padded>
              <Body />
            </CardContent>
          </Card>
        </Option>
        <Option name="inset" use="Sunk below its surroundings. Use for read-only details.">
          <Card inset>
            <CardContent padded>
              <Body />
            </CardContent>
          </Card>
        </Option>
        <Option name='border="default"' use="A stronger border, to sit next to inputs.">
          <Card border="default">
            <CardContent padded>
              <Body />
            </CardContent>
          </Card>
        </Option>
        <Option
          name="interactive"
          use="Highlights when it or anything inside it has focus. Click into the box."
        >
          <Card interactive>
            <CardContent padded>
              <label className="flex flex-col gap-1.5">
                <Text as="span" size="caption" color="muted">
                  Message
                </Text>
                <input
                  className="rounded-md border border-border bg-surface-elevated px-2 py-1 text-sm text-foreground"
                  defaultValue="Summarise today's alerts"
                />
              </label>
            </CardContent>
          </Card>
        </Option>
        <Option name="selected" use="Marks the chosen card in a set. Tinted fill.">
          <Card selected>
            <CardContent padded>
              <Body>Bedrock, chosen</Body>
            </CardContent>
          </Card>
        </Option>
        <Option
          name='selected selectedVariant="outline"'
          use="Marks the chosen card with a border only. Better for big cards."
        >
          <Card selected selectedVariant="outline">
            <CardContent padded>
              <Body>Bedrock, chosen</Body>
            </CardContent>
          </Card>
        </Option>
        <Option name="divide" use="Draws a line between each direct child, for lists of rows.">
          <Card divide>
            <CardContent padded compact>
              <Body>apollo-3</Body>
            </CardContent>
            <CardContent padded compact>
              <Body>apollo-4</Body>
            </CardContent>
          </Card>
        </Option>
        <Option
          name="toggleable"
          use="Shows a bar that opens and closes the body. Add defaultOpen to start open."
        >
          <Card toggleable>
            <CardHeader>
              <CardTitle>Setup</CardTitle>
              <CardDescription>Three steps to connect a tool</CardDescription>
            </CardHeader>
            <CardContent>
              <Body>Add the tool, grant access, then test the connection.</Body>
            </CardContent>
          </Card>
        </Option>
        <Option name="column" use="Stack children so the footer sits at the bottom of a tall card.">
          <Card column className="h-48">
            <CardHeader>
              <CardTitle>Costs</CardTitle>
            </CardHeader>
            <CardContent grow>
              <Body>$42.10 this month</Body>
            </CardContent>
            <CardFooter compact>
              <Text size="caption" color="muted">
                Updated 30 seconds ago
              </Text>
            </CardFooter>
          </Card>
        </Option>
      </OptionTable>

      <OptionTable title="CardHeader">
        <Option name="title, description" use="Short way to add a title and a line under it.">
          <Card>
            <CardHeader title="Behaviour" description="How the agent responds" />
          </Card>
        </Option>
        <Option name="leadingIcon" use="An icon before the title.">
          <Card>
            <CardHeader leadingIcon="settings" title="Behaviour" />
          </Card>
        </Option>
        <Option name="action" use="A button or badge at the far end of the header.">
          <Card>
            <CardHeader
              title="Behaviour"
              action={
                <Button size="sm" variant="ghost">
                  Edit
                </Button>
              }
            />
          </Card>
        </Option>
        <Option name="divider" use="A line between the header and the body.">
          <Card>
            <CardHeader title="Behaviour" divider />
            <CardContent padded>
              <Body />
            </CardContent>
          </Card>
        </Option>
        <Option
          name="icon and compact"
          use="A small icon box in the corner and tighter spacing. Use for stat cards."
        >
          <Card>
            <CardHeader compact icon={<Icon name="dollar-sign" size="sm" />}>
              <CardTitle>Costs</CardTitle>
              <CardDescription>Last 30 days</CardDescription>
            </CardHeader>
          </Card>
        </Option>
      </OptionTable>

      <OptionTable title="CardContent and CardFooter">
        <Option
          name="CardContent padded"
          use="Adds space at the top. Use it when there is no header."
        >
          <Card>
            <CardContent padded>
              <Body />
            </CardContent>
          </Card>
        </Option>
        <Option
          name="CardContent flush"
          use="Removes all space so a table or image reaches the edges."
        >
          <Card>
            <CardContent flush>
              <Body>Edge to edge</Body>
            </CardContent>
          </Card>
        </Option>
        <Option name="CardContent compact" use="Less space inside. Use in small panels.">
          <Card>
            <CardContent padded compact>
              <Body />
            </CardContent>
          </Card>
        </Option>
        <Option
          name='CardFooter justify="between"'
          use="Also start, center and end. Places the footer items."
        >
          <Card>
            <CardFooter justify="between" compact>
              <Text size="caption" color="muted">
                Showing 6 of 12
              </Text>
              <Button size="sm" variant="ghost">
                Load more
              </Button>
            </CardFooter>
          </Card>
        </Option>
      </OptionTable>
    </div>
  ),
};
