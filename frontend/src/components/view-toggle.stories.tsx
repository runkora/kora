import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { ViewToggle, type ViewMode } from './view-toggle';

/**
 * ViewToggle
 *
 * Switches a collection page between its card and list/table layouts. The page owns the state.
 */
const meta: Meta<typeof ViewToggle> = {
  title: 'App/Molecules/ViewToggle',
  component: ViewToggle,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof ViewToggle>;

function Stateful({ initial }: { initial: ViewMode }) {
  const [mode, setMode] = useState<ViewMode>(initial);
  return <ViewToggle viewMode={mode} onToggle={setMode} />;
}

export const Default: Story = {
  args: { viewMode: 'cards' },
  render: (args) => <Stateful initial={args.viewMode} />,
};

export const ListSelected: Story = {
  args: { viewMode: 'list' },
  render: (args) => <Stateful initial={args.viewMode} />,
};
