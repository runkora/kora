/**
 * Collapsible
 *
 * A Radix-backed expand/collapse region. Terra keeps it unstyled beyond
 * `data-slot` hooks — the composition provides the visuals (e.g. the agent
 * builder's per-server tool tree).
 */

import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Button } from '../button';
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from './index';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta: Meta<typeof Collapsible> = {
  title: 'Terra/Atoms/Collapsible',
  component: Collapsible,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Collapsible>;

// ─── Stories ──────────────────────────────────────────────────────────────────

/** Uncontrolled — starts open via `defaultOpen`, trigger toggles the region. */
export const Default: Story = {
  render: () => (
    <Collapsible defaultOpen className="w-[320px]">
      <Stack gap="snug">
        <CollapsibleTrigger asChild>
          <Button variant="ghost" size="sm">
            github-mcp — 4 tools
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <Stack gap="tight">
            <Text size="supporting" color="muted">
              create_issue
            </Text>
            <Text size="supporting" color="muted">
              list_pull_requests
            </Text>
            <Text size="supporting" color="muted">
              merge_pull_request
            </Text>
            <Text size="supporting" color="muted">
              search_code
            </Text>
          </Stack>
        </CollapsibleContent>
      </Stack>
    </Collapsible>
  ),
};

/** Collapsed by default — content is hidden until the trigger is activated. */
export const Collapsed: Story = {
  render: () => (
    <Collapsible className="w-[320px]">
      <Stack gap="snug">
        <CollapsibleTrigger asChild>
          <Button variant="ghost" size="sm">
            slack-mcp — 2 tools
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <Stack gap="tight">
            <Text size="supporting" color="muted">
              post_message
            </Text>
            <Text size="supporting" color="muted">
              list_channels
            </Text>
          </Stack>
        </CollapsibleContent>
      </Stack>
    </Collapsible>
  ),
};
