import type { Meta, StoryObj } from '@storybook/react';
import {
  budgetRun,
  doneRun,
  eventStartedRun,
  joinedRun,
} from '../_lib/run-page-proposed-mock-data';
import { RunMixPage } from './run-mix-proposed';

/**
 * The proposed run page: sessions on the left, the thread in the middle with trigger messages
 * shown as events, the status composer at its foot, and the run's details on the right.
 */
const meta: Meta<typeof RunMixPage> = {
  title: 'Pages/Agents/Run (mix)',
  component: RunMixPage,
  parameters: {
    layout: 'fullscreen',
    nextjs: {
      appDirectory: true,
      navigation: { pathname: '/agents/code-reviewer/runs/3f2c9a10-5b7e-4d21-9c8a-6e1f0b7d2a44' },
    },
  },
};

export default meta;
type Story = StoryObj<typeof RunMixPage>;

/** The Code Reviewer's earlier runs, for the sessions pane. */
const REVIEWER_EARLIER = [
  { label: 'Review pull request #5 in example-org/stats-lib adding Mode', meta: '1d' },
  { label: 'Review pull request #4 fixing Mean on an empty slice', meta: '3d' },
  { label: 'Review pull request #3 in example-org/stats-lib adding Max', meta: '7d' },
];

/** Mid-run: a GitHub mention has joined, and a message waits for the agent's next step. */
export const Running: Story = {
  render: () => (
    <RunMixPage
      run={{
        ...joinedRun(),
        queued: [{ id: 'q1', text: 'Also check Median on an empty slice before you decide.' }],
      }}
      earlier={REVIEWER_EARLIER}
    />
  ),
};

/** Finished: the composer gives way to the result line. */
export const Done: Story = {
  render: () => <RunMixPage run={doneRun()} earlier={REVIEWER_EARLIER} />,
};

/** A run started by a platform event, read from its prompt and data block. */
export const EventStarted: Story = {
  render: () => <RunMixPage run={eventStartedRun()} earlier={[]} />,
};

/** Stopped because the agent's daily budget ran out: says whose budget, and when it resets. */
export const BudgetExhausted: Story = {
  render: () => <RunMixPage run={budgetRun()} earlier={REVIEWER_EARLIER} />,
};
