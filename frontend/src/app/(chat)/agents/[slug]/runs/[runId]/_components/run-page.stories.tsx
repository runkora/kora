import type { Meta, StoryObj } from '@storybook/react';
import {
  BUDGETS,
  budgetRun,
  doneRun,
  eventStartedRun,
  runStory,
  runningRun,
} from '../_lib/run-page-mock-data';
import { RunShell } from './run-shell';

/**
 * The run page: sessions on the left, the thread in the middle with trigger messages shown as
 * events, the status composer at its foot, and the run's details on the right. Each story serves
 * the run and its recorded events through the mock BFF, as on a reload.
 */
const meta: Meta<typeof RunShell> = {
  title: 'Pages/Agents/Run',
  component: RunShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: {
      appDirectory: true,
      navigation: { pathname: '/agents/code-reviewer/runs/3f2c9a10-5b7e-4d21-9c8a-6e1f0b7d2a44' },
    },
  },
};

export default meta;
type Story = StoryObj<typeof RunShell>;

const args = (run: ReturnType<typeof runningRun>) => ({
  slug: run.body.agent_type.slug ?? 'code-reviewer',
  runId: run.body.data.id,
});

/** Mid-run: a GitHub mention has joined the working run. */
export const Running: Story = { args: args(runningRun()), decorators: runStory([runningRun()]) };

/** Finished: the composer gives way to the result line. */
export const Done: Story = { args: args(doneRun()), decorators: runStory([doneRun()]) };

/** A run started by a platform event, read from its prompt and data block. */
export const EventStarted: Story = {
  args: args(eventStartedRun()),
  decorators: runStory([eventStartedRun()], []),
};

/** Stopped because the agent's monthly budget ran out: says whose budget, and when it resets. */
export const BudgetExhausted: Story = {
  args: args(budgetRun(BUDGETS.monthly)),
  decorators: runStory([budgetRun(BUDGETS.monthly)]),
};

/** Stopped by the run's own ceiling, which has no owner to name and does not reset. */
export const BudgetExhaustedPerRun: Story = {
  args: args(budgetRun(BUDGETS.perRun)),
  decorators: runStory([budgetRun(BUDGETS.perRun)]),
};

/** Stopped by the tenant's budget, which has no name to give: named by its level. */
export const BudgetExhaustedTenant: Story = {
  args: args(budgetRun(BUDGETS.tenant)),
  decorators: runStory([budgetRun(BUDGETS.tenant)]),
};

/** Stopped by a budget the run did not record: a generic stop line. */
export const BudgetExhaustedUnrecorded: Story = {
  args: args(budgetRun()),
  decorators: runStory([budgetRun()]),
};
