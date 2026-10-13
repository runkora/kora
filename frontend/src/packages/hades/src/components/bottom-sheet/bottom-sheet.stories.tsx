import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Button } from '../button/button';
import { Icon } from '../icon/icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { BottomSheet } from './bottom-sheet';

/**
 * BottomSheet
 *
 * A panel that slides up from the bottom of the screen. Use it on a phone where a menu or a short
 * form would otherwise be a popover. Drag the tab down, tap outside, press Escape or use the close
 * button to dismiss it.
 */
const meta: Meta<typeof BottomSheet> = {
  title: 'Hades/Molecules/BottomSheet',
  component: BottomSheet,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof BottomSheet>;

function SheetDemo({ label, children }: { label: string; children: React.ReactNode }) {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {label}
      </Button>
      <BottomSheet open={open} onOpenChange={setOpen} title="Example sheet">
        {children}
      </BottomSheet>
    </>
  );
}

function Actions() {
  return (
    <Stack gap="snug" padding="snug">
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
    </Stack>
  );
}

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="A short menu">
        <SheetDemo label="Open bottom sheet">
          <Actions />
        </SheetDemo>
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="Options">
        <Option
          name="open, onOpenChange"
          use="Controls whether the sheet is showing. Every way of dismissing it calls onOpenChange(false)."
        >
          <SheetDemo label="Open">
            <Actions />
          </SheetDemo>
        </Option>
        <Option name="title" use="The accessible name of the sheet. It is not shown on screen.">
          <SheetDemo label="Open">
            <Actions />
          </SheetDemo>
        </Option>
        <Option name="children" use="The content. The sheet grows to fit it from the bottom.">
          <SheetDemo label="Open tall sheet">
            <Stack gap="snug" padding="comfortable">
              {Array.from({ length: 6 }, (_, i) => (
                <Text key={i} size="supporting">
                  Row {i + 1}
                </Text>
              ))}
            </Stack>
          </SheetDemo>
        </Option>
      </OptionTable>
    </Stack>
  ),
};
