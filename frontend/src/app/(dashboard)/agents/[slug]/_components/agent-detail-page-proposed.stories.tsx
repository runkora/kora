// Design mock for the Grader feature. The real implementation later in this stack replaces this file.

import type { Meta, StoryObj } from '@storybook/react';
import { CODE_REVIEWER } from '../../_components/agents-mock-data';
import { fleetSeries } from '../../_lib/grades-proposed';
import { rowFor, runRowsFor, withRoutes } from './agent-detail-page-mock-data';
import { mockOverview } from './agent-overview-mock-data';
import { AgentDetailShellProposed } from './agent-detail-shell-proposed';

/** Proposed: an agent's Graders in the Grades tab, and one Grader's page. */
const meta: Meta<typeof AgentDetailShellProposed> = {
  title: 'Proposed/Graders/Agent page',
  component: AgentDetailShellProposed,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/agents/code-reviewer' } },
  },
  decorators: withRoutes(CODE_REVIEWER),
  args: {
    initialTab: 'grades',
    initialAgent: rowFor(CODE_REVIEWER),
    initialRuns: runRowsFor(CODE_REVIEWER),
    initialNextCursor: null,
    initialError: null,
    initialStatus: 'success',
    initialOverview: mockOverview(CODE_REVIEWER.slug, '7d'),
  },
};

export default meta;
type Story = StoryObj<typeof AgentDetailShellProposed>;

/** The Grades tab: every Grader, failing first. */
export const Checks: Story = {
  name: 'Checks',
  args: { grades: fleetSeries() },
};

/** The Grades tab for an agent with no Graders. */
export const ChecksNoGraders: Story = {
  name: 'Checks, no graders',
  args: { grades: [] },
};

/**
 * One Grader: five questions, one of them often uncertain, with its failed or uncertain runs.
 *
 * "All failed runs for this question" opens the Runs tab with `?grader=&question=&verdict=fail`.
 * That needs backend support: the runs list does not filter by grade yet.
 */
export const CheckPage: Story = {
  name: 'Check page',
  args: { grades: fleetSeries(), initialGrader: 'summary-accurate' },
};

/** One Grader with a single question: its failed or uncertain runs follow the question. */
export const CheckPageOneQuestion: Story = {
  name: 'Check page, one question',
  args: { grades: fleetSeries(), initialGrader: 'no-push' },
};

/** The Grades tab with 40 Graders: paged, searchable and filtered by status. */
export const ManyGraders: Story = {
  name: 'Many Graders',
  args: { grades: fleetSeries('many') },
};
