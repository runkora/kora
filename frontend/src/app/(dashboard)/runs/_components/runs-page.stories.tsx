import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage } from '@runkora/storybook/dashboard-frame';
import { mockEventSource } from '@runkora/storybook/mock-event-source';
import { AGENT_TYPES } from '../../agents/_components/agents-mock-data';
import { RUN_CREATED } from '../_lib/runs-feed';
import { RunsPage } from './runs-page';
import {
  buildRuns,
  failingReloadRoutes,
  failingRunsRoutes,
  liveRunEvents,
  pendingRunsRoutes,
  resetOnce,
  runEvent,
  runsRoutes,
} from './runs-mock-data';

/** The Runs tab: every run the viewer can see as one line, newest first, kept live. */
const meta: Meta<typeof RunsPage> = {
  title: 'Pages/Runs',
  component: RunsPage,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/runs' } },
  },
};

export default meta;
type Story = StoryObj<typeof RunsPage>;

const quiet = mockEventSource([]);

export const Default: Story = {
  decorators: [dashboardPage(runsRoutes(buildRuns())), quiet],
};

/** Two pages of runs, with Newer and Older below the list. */
export const ManyRuns: Story = {
  decorators: [dashboardPage(runsRoutes(buildRuns({ runs: 80 }))), quiet],
};

export const Empty: Story = {
  decorators: [dashboardPage(runsRoutes([])), quiet],
};

export const LoadError: Story = {
  decorators: [dashboardPage(failingRunsRoutes(502)), quiet],
};

/** The agent list failed, so the Agent filter says so in place of its select. */
export const AgentsLoadError: Story = {
  decorators: [
    dashboardPage({
      ...runsRoutes(buildRuns()),
      'GET /api/agents/types': () => new Response(null, { status: 502 }),
    }),
    quiet,
  ],
};

/** A long agent name truncates in its badge, with the full name as its tooltip. */
export const LongAgentName: Story = {
  decorators: [
    dashboardPage({
      ...runsRoutes(buildRuns()),
      'GET /api/agents/types': {
        data: AGENT_TYPES.map((a) =>
          a.id === 'at_coder'
            ? { ...a, name: 'Platform dependency upgrade and database migration coder' }
            : a,
        ),
      },
    }),
    quiet,
  ],
};

/** The Source select keeps the runs webhooks started. */
export const FilteredBySource: Story = {
  decorators: [dashboardPage(runsRoutes(buildRuns())), quiet],
  play: async ({ canvasElement }) => {
    await userEvent.selectOptions(
      await within(canvasElement).findByLabelText('Filter by source'),
      'webhook',
    );
  },
};

/** A search that matches nothing offers to clear the filters. */
export const NoMatches: Story = {
  decorators: [dashboardPage(runsRoutes(buildRuns())), quiet],
  play: async ({ canvasElement }) => {
    await userEvent.type(
      await within(canvasElement).findByPlaceholderText('Search runs…'),
      'no such run',
    );
  },
};

/** The stream resets and the reload fails: the loaded rows stay under the error. */
export const ReloadError: Story = {
  decorators: [dashboardPage(failingReloadRoutes(buildRuns())), mockEventSource(resetOnce(1500))],
};

export const Loading: Story = {
  decorators: [dashboardPage(pendingRunsRoutes), quiet],
};

/** On the first page, a new run appears at the top, then starts and gets its title. */
export const LiveInsert: Story = {
  decorators: [dashboardPage(runsRoutes(buildRuns())), mockEventSource(liveRunEvents(1500))],
};

/** On a later page, new runs wait behind the bar and the rows stay where they are. */
export const NewRunsBar: Story = {
  decorators: [
    dashboardPage(runsRoutes(buildRuns({ runs: 80 }))),
    mockEventSource(() => {
      const [a, b] = buildRuns({ runs: 2 });
      return [
        runEvent({ ...a!, id: 'run_new0001' }, RUN_CREATED, 2000),
        runEvent({ ...b!, id: 'run_new0002' }, RUN_CREATED, 2200),
      ];
    }),
  ],
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole('button', { name: /Older/ }));
  },
};
