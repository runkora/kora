import type { Meta, StoryObj } from '@storybook/react';
import { Badge } from '../badge/badge';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from './card';

const meta: Meta<typeof Card> = {
  title: 'Terra/Atoms/Card',
  component: Card,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Card>;

export const Default: Story = {
  render: () => (
    <Card className="w-80">
      <CardHeader>
        <CardTitle>Agent: apollo-3</CardTitle>
        <CardDescription>Last active 2 minutes ago</CardDescription>
      </CardHeader>
      <CardContent>
        <Text size="supporting" color="muted">
          Processing 14 pending tasks across 3 connected tools.
        </Text>
      </CardContent>
    </Card>
  ),
};

export const WithFooter: Story = {
  render: () => (
    <Card className="w-80">
      <CardHeader>
        <CardTitle>Credential broker</CardTitle>
        <CardDescription>AWS us-east-1 · expires in 12 min</CardDescription>
      </CardHeader>
      <CardContent>
        <Text size="supporting" color="muted">
          Scoped read-only access to S3 and DynamoDB.
        </Text>
      </CardContent>
      <CardFooter className="justify-between">
        <Button variant="ghost" size="sm">
          Revoke
        </Button>
        <Button variant="secondary" size="sm">
          Renew
        </Button>
      </CardFooter>
    </Card>
  ),
};

export const WithBadge: Story = {
  render: () => (
    <Card className="w-80">
      <CardHeader>
        <div className="flex items-start justify-between gap-2">
          <CardTitle>Policy engine</CardTitle>
          <Badge variant="healthy" dot>
            Healthy
          </Badge>
        </div>
        <CardDescription>OPA v0.63 · 847 rules loaded</CardDescription>
      </CardHeader>
      <CardContent>
        <Text size="supporting" color="muted">
          Evaluated 2,341 decisions in the last hour. Zero violations.
        </Text>
      </CardContent>
    </Card>
  ),
};

export const WithIcon: Story = {
  render: () => (
    <Card column className="w-72">
      <CardHeader compact icon={<Icon name="activity" size="sm" />}>
        <CardTitle>Total Requests</CardTitle>
        <CardDescription>Last billing period</CardDescription>
      </CardHeader>
      <CardContent>
        <Text size="supporting" color="muted">
          1.2M requests · 98.4% success rate
        </Text>
      </CardContent>
    </Card>
  ),
};

export const ElevatedSurface: Story = {
  render: () => (
    <Card className="w-80 bg-surface-elevated">
      <CardHeader>
        <CardTitle>Elevated card</CardTitle>
        <CardDescription>Uses bg-surface-elevated for nested depth</CardDescription>
      </CardHeader>
      <CardContent>
        <Text size="supporting" color="muted">
          Place this inside another surface to express a second level of depth.
        </Text>
      </CardContent>
    </Card>
  ),
};

/**
 * Section card anatomy — `leadingIcon`, `title`, `description`, `action`, and
 * `divider` all provided to `CardHeader`. This is the pattern used for settings
 * sections, configuration panels, and any card where the header identifies a
 * named configuration area.
 */
export const SectionCardAnatomy: Story = {
  render: () => (
    <Stack direction="vertical" gap="comfortable" className="w-[480px]">
      {/* With description and action */}
      <Card>
        <CardHeader
          leadingIcon="user"
          title="Identity"
          description="Display name, slug, and framework selection."
          action={
            <Button variant="ghost" size="sm">
              Edit
            </Button>
          }
          divider
        />
        <CardContent padded>
          <Text size="supporting" color="muted">
            Name, slug, description, and framework fields go here.
          </Text>
        </CardContent>
      </Card>

      {/* With description, no action */}
      <Card>
        <CardHeader
          leadingIcon="cpu"
          title="Model"
          description="Choose the language model powering this agent."
          divider
        />
        <CardContent padded>
          <Text size="supporting" color="muted">
            Model picker goes here.
          </Text>
        </CardContent>
      </Card>

      {/* No description */}
      <Card>
        <CardHeader leadingIcon="wrench" title="Tools" divider />
        <CardContent padded>
          <Text size="supporting" color="muted">
            Tool permission rows go here.
          </Text>
        </CardContent>
      </Card>

      {/* Danger variant — no leading icon, action only */}
      <Card>
        <CardHeader
          title="Danger Zone"
          action={
            <Badge variant="destructive" size="sm">
              Irreversible
            </Badge>
          }
          divider
        />
        <CardContent padded>
          <Text size="supporting" color="muted">
            Destructive actions go here.
          </Text>
        </CardContent>
      </Card>
    </Stack>
  ),
};

/**
 * Multiple section cards stacked — shows the full page density and how the
 * divider + tighter padding compose across a real settings layout.
 */
export const SectionCardStack: Story = {
  render: () => (
    <Stack direction="vertical" gap="snug" className="w-[520px]">
      {(['user', 'cpu', 'file-text', 'hard-drive', 'wrench', 'settings'] as const).map(
        (icon, i) => {
          const titles = [
            'Identity',
            'Model',
            'System Prompt',
            'Context & Memory',
            'Tools',
            'Behaviour',
          ];
          const descs = [
            'Agent display name, slug, and framework.',
            'Language model powering this agent.',
            'Instructions prepended to every conversation.',
            'Persistence and context window configuration.',
            'Tool permission levels for each capability.',
            'Retry, streaming, and safety settings.',
          ];
          return (
            <Card key={icon}>
              <CardHeader leadingIcon={icon} title={titles[i]} description={descs[i]} divider />
              <CardContent padded>
                <Text size="supporting" color="muted">
                  Section content
                </Text>
              </CardContent>
            </Card>
          );
        },
      )}
    </Stack>
  ),
};

/**
 * The `interactive` prop adds a `focus-within:border-accent` ring so the card
 * responds visually when a focusable descendant (an input, textarea, or button)
 * receives keyboard focus. Use for input cards, chat boxes, and search surfaces
 * where the card itself acts as the focus container.
 *
 * Click or tab into the textarea below to see the accent border activate.
 */
export const Interactive: Story = {
  render: () => (
    <Card interactive className="w-96">
      <CardContent padded>
        <Stack direction="vertical" gap="snug">
          <Text size="caption" color="subtle" weight="strong" transform="uppercase">
            Ask Orbit AI
          </Text>
          <textarea
            className="w-full resize-none bg-transparent text-sm text-foreground placeholder:text-muted-foreground focus:outline-none"
            rows={3}
            placeholder="Type a message&hellip;"
          />
        </Stack>
      </CardContent>
      <CardFooter justify="between" compact>
        <Text size="micro" color="subtle">
          ⌘↵ to send
        </Text>
        <Button size="sm" variant="default">
          Send
        </Button>
      </CardFooter>
    </Card>
  ),
};

/**
 * The four `CardFooter` justify modes — `start` (default), `center`, `between`,
 * and `end` — control the horizontal distribution of footer content.
 * The `compact` flag reduces vertical padding for dense card layouts.
 */
export const CardFooterVariants: Story = {
  name: 'CardFooter — justify variants',
  render: () => (
    <Stack direction="vertical" gap="comfortable" className="w-80">
      <Text size="caption" color="subtle" weight="strong" transform="uppercase">
        justify start (default)
      </Text>
      <Card>
        <CardFooter justify="start">
          <Button size="sm" variant="secondary">
            Cancel
          </Button>
          <Button size="sm" variant="default">
            Save
          </Button>
        </CardFooter>
      </Card>

      <Text size="caption" color="subtle" weight="strong" transform="uppercase">
        justify center
      </Text>
      <Card>
        <CardFooter justify="center">
          <Button size="sm" variant="default">
            Get started
          </Button>
        </CardFooter>
      </Card>

      <Text size="caption" color="subtle" weight="strong" transform="uppercase">
        justify between
      </Text>
      <Card>
        <CardFooter justify="between">
          <Button size="sm" variant="ghost">
            Revoke
          </Button>
          <Button size="sm" variant="default">
            Renew
          </Button>
        </CardFooter>
      </Card>

      <Text size="caption" color="subtle" weight="strong" transform="uppercase">
        justify end
      </Text>
      <Card>
        <CardFooter justify="end">
          <Button size="sm" variant="default">
            Save changes
          </Button>
        </CardFooter>
      </Card>

      <Text size="caption" color="subtle" weight="strong" transform="uppercase">
        justify between + compact
      </Text>
      <Card>
        <CardFooter justify="between" compact>
          <Text size="caption" color="subtle">
            Last updated 2 min ago
          </Text>
          <Button size="sm" variant="ghost">
            Refresh
          </Button>
        </CardFooter>
      </Card>
    </Stack>
  ),
};
