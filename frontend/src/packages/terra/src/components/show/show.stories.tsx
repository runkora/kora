import type { Meta, StoryObj } from '@storybook/react';
import { Option, OptionTable } from '../../docs/story-ui';
import { Text } from '../typography/typography';
import { Show } from './show';

/**
 * Show
 *
 * Shows or hides content by screen width. Give it exactly one of `above` or
 * `below`. Resize the preview to see each sample change.
 */
const meta: Meta<typeof Show> = {
  title: 'Terra/Layout/Show',
  component: Show,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Show>;

/** Shown while its rule is true, and a grey note while it is not. */
function Result({ visible, hidden }: { visible: React.ReactNode; hidden: React.ReactNode }) {
  return (
    <>
      {visible}
      {hidden}
    </>
  );
}

const on = (label: string) => (
  <span className="rounded-md border border-accent bg-accent-subtle px-3 py-1.5 text-sm text-foreground">
    {label}
  </span>
);
const off = (label: string) => (
  <Text as="span" size="supporting" color="subtle">
    {label}
  </Text>
);

export const Overview: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Breakpoints"
        note="Breakpoints: sm is 640px, md is 768px, lg is 1024px, xl is 1280px. Each sample reads Visible or Hidden for the width of the preview."
      >
        <Option name='above="sm"' use="Hidden on phones. Visible from 640px up.">
          <Result
            visible={<Show above="sm">{on('Visible')}</Show>}
            hidden={<Show below="sm">{off('Hidden')}</Show>}
          />
        </Option>
        <Option name='above="lg"' use="Only on wide screens. Visible from 1024px up.">
          <Result
            visible={<Show above="lg">{on('Visible')}</Show>}
            hidden={<Show below="lg">{off('Hidden')}</Show>}
          />
        </Option>
        <Option name='below="md"' use="Only on small screens. Hidden from 768px up.">
          <Result
            visible={<Show below="md">{on('Visible')}</Show>}
            hidden={<Show above="md">{off('Hidden')}</Show>}
          />
        </Option>
        <Option name='below="xl"' use="Hidden on very wide screens. Hidden from 1280px up.">
          <Result
            visible={<Show below="xl">{on('Visible')}</Show>}
            hidden={<Show above="xl">{off('Hidden')}</Show>}
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
