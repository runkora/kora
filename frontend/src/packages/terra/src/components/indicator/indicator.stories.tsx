/**
 * Indicator
 *
 * Wraps a child and overlays a small status dot in its top-right corner.
 * The canonical use-case is a notification badge on an icon button — the
 * dot signals unread items without needing a counter.
 *
 * `Indicator` must wrap a single child that accepts `ref` forwarding; a
 * `<Button>` or any `<HTMLElement>` works. The dot is `aria-hidden` because
 * the parent button's `aria-label` should carry the full notification intent.
 *
 * ```tsx
 * <Indicator status="error">
 *   <Button variant="ghost" size="icon" aria-label="Notifications (3 unread)">
 *     <Icon name="bell" />
 *   </Button>
 * </Indicator>
 * ```
 */

import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Indicator } from './indicator';

// ─── Meta ─────────────────────────────────────────────────────────────────────

const meta: Meta<typeof Indicator> = {
  title: 'Terra/Atoms/Indicator',
  component: Indicator,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
  argTypes: {
    status: {
      control: 'select',
      options: ['healthy', 'degraded', 'error', 'idle', 'pending'],
    },
  },
};

export default meta;
type Story = StoryObj<typeof Indicator>;

// ─── Stories ──────────────────────────────────────────────────────────────────

/** Interactive default — switch the status via the Controls panel. */
export const Default: Story = {
  args: { status: 'error' },
  render: (args) => (
    <Indicator {...args}>
      <Button variant="ghost" size="icon" aria-label="Notifications">
        <Icon name="bell" />
      </Button>
    </Indicator>
  ),
};

/**
 * All five status variants. Each maps to a `--color-status-*` token so the
 * dot colour carries operational meaning matching the rest of the system.
 */
export const StatusVariants: Story = {
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center">
      {(
        [
          { status: 'error', icon: 'bell', label: 'Notifications (error)' },
          { status: 'pending', icon: 'clock', label: 'Scheduled (pending)' },
          { status: 'degraded', icon: 'alert-circle', label: 'Alerts (degraded)' },
          { status: 'healthy', icon: 'activity', label: 'Status (healthy)' },
          { status: 'idle', icon: 'settings', label: 'Config (idle)' },
        ] as const
      ).map(({ status, icon, label }) => (
        <Stack key={status} gap="tight" align="center">
          <Indicator status={status}>
            <Button variant="ghost" size="icon" aria-label={label}>
              <Icon name={icon} />
            </Button>
          </Indicator>
          <Text size="micro" color="muted">
            {status}
          </Text>
        </Stack>
      ))}
    </Stack>
  ),
};

/**
 * Indicator wraps any single child, not just Buttons. Here it overlays a dot
 * on a plain icon — useful for feature icons in a sidebar or card header.
 */
export const OnIcon: Story = {
  name: 'On a feature icon',
  render: () => (
    <Indicator status="error">
      <Icon name="shield" size="lg" container containerVariant="surface" />
    </Indicator>
  ),
};
