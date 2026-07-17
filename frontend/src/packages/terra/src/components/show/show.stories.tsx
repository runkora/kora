/**
 * Show
 *
 * Responsive visibility primitive. Replaces `hidden sm:block` wrapper divs
 * with an intent-explicit component. Exactly one of `above` or `below` must
 * be provided — they are mutually exclusive at the type level.
 *
 *   above="sm"  → hidden by default, visible at sm (640 px) and above
 *   below="md"  → visible by default, hidden at md (768 px) and above
 *
 * Resize the Storybook canvas to see the responsive behaviour in action.
 */

import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Show } from './show';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta: Meta<typeof Show> = {
  title: 'Terra/Layout/Show',
  component: Show,
  parameters: {
    layout: 'padded',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Show>;

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Visual indicator box shown when content is visible at the current width. */
function Indicator({ label }: { label: string }) {
  return (
    <div className="flex items-center justify-center rounded-lg border border-accent/30 bg-accent/10 px-4 py-3">
      <Text as="span" size="supporting" color="accent" weight="emphasis">
        {label}
      </Text>
    </div>
  );
}

// ─── Stories ──────────────────────────────────────────────────────────────────

/**
 * `above="sm"` hides content on viewports narrower than 640 px and shows it
 * at 640 px and above. Resize the canvas below 640 px to see it disappear.
 *
 * Common use cases: keyboard shortcuts, sidebar detail panels, expanded table
 * columns — things that only make sense on larger screens.
 */
export const AboveSm: Story = {
  name: 'above="sm" — hidden on mobile',
  render: () => (
    <Stack direction="vertical" gap="comfortable">
      <Text size="supporting" color="muted">
        Resize the canvas to below 640 px — the box below will disappear.
      </Text>
      <Show above="sm">
        <Indicator label="Visible at sm (640 px) and above" />
      </Show>
    </Stack>
  ),
};

/**
 * `below="md"` shows content below 768 px and hides it at 768 px and above.
 * Resize the canvas above 768 px to see it disappear.
 *
 * Common use cases: mobile navigation, condensed hero copy, bottom action
 * sheets — things that only belong on small screens.
 */
export const BelowMd: Story = {
  name: 'below="md" — hidden on desktop',
  render: () => (
    <Stack direction="vertical" gap="comfortable">
      <Text size="supporting" color="muted">
        Resize the canvas above 768 px — the box below will disappear.
      </Text>
      <Show below="md">
        <Indicator label="Visible below md (768 px)" />
      </Show>
    </Stack>
  ),
};

/**
 * All four `above` breakpoints documented side-by-side.
 * Each indicator names the exact pixel threshold it responds to.
 */
export const AllAboveBreakpoints: Story = {
  name: 'All above breakpoints',
  render: () => (
    <Stack direction="vertical" gap="snug">
      <Text size="caption" color="subtle" weight="strong" transform="uppercase">
        Hidden below / visible above
      </Text>
      <Show above="sm">
        <Indicator label='above="sm" — visible at ≥ 640 px' />
      </Show>
      <Show above="md">
        <Indicator label='above="md" — visible at ≥ 768 px' />
      </Show>
      <Show above="lg">
        <Indicator label='above="lg" — visible at ≥ 1024 px' />
      </Show>
      <Show above="xl">
        <Indicator label='above="xl" — visible at ≥ 1280 px' />
      </Show>
    </Stack>
  ),
};

/**
 * All four `below` breakpoints documented side-by-side.
 */
export const AllBelowBreakpoints: Story = {
  name: 'All below breakpoints',
  render: () => (
    <Stack direction="vertical" gap="snug">
      <Text size="caption" color="subtle" weight="strong" transform="uppercase">
        Visible below / hidden above
      </Text>
      <Show below="sm">
        <Indicator label='below="sm" — hidden at ≥ 640 px' />
      </Show>
      <Show below="md">
        <Indicator label='below="md" — hidden at ≥ 768 px' />
      </Show>
      <Show below="lg">
        <Indicator label='below="lg" — hidden at ≥ 1024 px' />
      </Show>
      <Show below="xl">
        <Indicator label='below="xl" — hidden at ≥ 1280 px' />
      </Show>
    </Stack>
  ),
};

/**
 * Real-world composition: a chat input footer showing a keyboard hint only on
 * larger screens, and a tap-to-send hint only on smaller screens — the same
 * pattern used in `chat/page.tsx`.
 */
export const InContext: Story = {
  name: 'In Context — responsive chat hint',
  render: () => (
    <div className="w-full max-w-md rounded-xl border border-border-subtle bg-surface p-3">
      <div className="flex items-end justify-between gap-2">
        <Text size="supporting" color="muted">
          Ask Orbit AI anything…
        </Text>
        <Stack direction="horizontal" gap="snug" align="center">
          <Show above="sm">
            <Text size="micro" color="subtle">
              ⌘↵ to send
            </Text>
          </Show>
          <Show below="sm">
            <Text size="micro" color="subtle">
              Tap ↑ to send
            </Text>
          </Show>
        </Stack>
      </div>
    </div>
  ),
};
