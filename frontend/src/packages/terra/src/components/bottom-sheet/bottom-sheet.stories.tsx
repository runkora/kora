import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Text } from '../typography/typography';
import { BottomSheet } from './bottom-sheet';

const meta: Meta<typeof BottomSheet> = {
  title: 'Terra/Organisms/BottomSheet',
  component: BottomSheet,
  parameters: {
    layout: 'fullscreen',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof BottomSheet>;

// ── Controlled wrapper ────────────────────────────────────────────────────────

const SheetTrigger = ({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) => {
  const [open, setOpen] = React.useState(false);
  return (
    <div style={{ minHeight: '60vh', display: 'flex', alignItems: 'flex-end', padding: 16 }}>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {label}
      </Button>
      <BottomSheet open={open} onOpenChange={setOpen} title="Example sheet">
        {children}
      </BottomSheet>
    </div>
  );
};

// ── Stories ───────────────────────────────────────────────────────────────────

/**
 * Drag the pull tab down to dismiss (releases past 30% of the sheet's height,
 * or a fast flick, close it — otherwise it springs back). The × button, the
 * scrim, and Escape all close it too — dismissal never depends on the drag
 * gesture alone.
 */
export const Default: Story = {
  render: () => (
    <SheetTrigger label="Open bottom sheet">
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: '4px 8px 16px' }}>
        <Button variant="ghost" size="sm">
          <Icon name="user" size="sm" />
          <Text as="span" size="supporting">
            View profile
          </Text>
        </Button>
        <Button variant="ghost" size="sm">
          <Icon name="log-out" size="sm" color="status-error" />
          <Text as="span" size="supporting" color="status-error">
            Log out
          </Text>
        </Button>
      </div>
    </SheetTrigger>
  ),
};

/**
 * Taller content to demonstrate the sheet growing from the bottom while
 * staying anchored — the sheet's height is content-driven.
 */
export const TallContent: Story = {
  render: () => (
    <SheetTrigger label="Open tall sheet">
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: '4px 20px 16px' }}>
        {Array.from({ length: 6 }, (_, i) => (
          <Text key={i} size="supporting">
            Row {i + 1}
          </Text>
        ))}
      </div>
    </SheetTrigger>
  ),
};
