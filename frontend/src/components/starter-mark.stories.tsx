import type { Meta, StoryObj } from '@storybook/react';
import { TooltipProvider } from '@runkora/hades';
import { StarterMark } from './starter-mark';

/** Who started a run: a person's initials, or the icon of the trigger kind that did. */
const meta: Meta<typeof StarterMark> = {
  title: 'App/Molecules/StarterMark',
  component: StarterMark,
  tags: ['autodocs'],
  // The mark's tooltip needs the provider the dashboard layout supplies.
  decorators: [
    (Story) => (
      <TooltipProvider>
        <Story />
      </TooltipProvider>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof StarterMark>;

export const Person: Story = {
  args: { row: { createdBy: 'usr_dana', createdByName: 'Dana Lee', startedByTrigger: null } },
};

export const Trigger: Story = {
  args: {
    row: {
      createdBy: 'trg_nightly',
      createdByName: null,
      startedByTrigger: {
        triggerId: 'trg_nightly',
        triggerName: 'nightly dependency scan',
        triggerKind: 'scheduled',
      },
    },
  },
};
