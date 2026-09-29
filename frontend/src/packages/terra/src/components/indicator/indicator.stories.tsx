import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { Option, OptionTable } from '../../docs/story-ui';
import { Indicator } from './indicator';

/**
 * Indicator
 *
 * Puts a small status dot on the top right corner of another element. Use it to show that
 * something needs attention, such as unread notifications.
 */
const meta: Meta<typeof Indicator> = {
  title: 'Terra/Molecules/Indicator',
  component: Indicator,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Indicator>;

export const Overview: Story = {
  render: () => (
    <Stack direction="horizontal" gap="relaxed" align="center">
      <Stack gap="tight" align="center">
        <Indicator status="error">
          <Button variant="ghost" size="icon" aria-label="Notifications, 3 unread">
            <Icon name="bell" />
          </Button>
        </Indicator>
        <Text size="caption" color="muted">
          With unread
        </Text>
      </Stack>
      <Stack gap="tight" align="center">
        <Button variant="ghost" size="icon" aria-label="Notifications, none unread">
          <Icon name="bell" />
        </Button>
        <Text size="caption" color="muted">
          None unread
        </Text>
      </Stack>
    </Stack>
  ),
};

const STATUSES = [
  ['error', 'Something failed and needs action. The default.'],
  ['degraded', 'Something is working but not well.'],
  ['pending', 'Something is waiting or in progress.'],
  ['healthy', 'Everything is fine.'],
  ['idle', 'Nothing is happening.'],
] as const;

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Give the wrapped element an aria-label that says what the dot means. The dot itself is hidden from screen readers."
      >
        {STATUSES.map(([status, use]) => (
          <Option key={status} name={`status="${status}"`} use={use}>
            <Indicator status={status}>
              <Button variant="ghost" size="icon" aria-label={`Alerts, status ${status}`}>
                <Icon name="bell" />
              </Button>
            </Indicator>
          </Option>
        ))}
      </OptionTable>
    </div>
  ),
};
