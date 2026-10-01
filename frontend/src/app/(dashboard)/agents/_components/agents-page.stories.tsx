import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import type { MockRoutes } from '@orbit/storybook/mock-api';
import AgentsLayout from '../layout';
import { AgentsShell } from './agents-shell';
import {
  AGENT_ROWS,
  AGENT_TYPES,
  DRAFTS,
  MANY_AGENT_ROWS,
  MANY_AGENT_TYPES,
  MANY_RAW_RUNS,
  MANY_RUN_ROWS,
  RAW_RUNS,
  RUN_ROWS,
  USAGE,
} from './agents-mock-data';

/** The routes the list page calls on mount and on refresh. */
function listRoutes(overrides: MockRoutes = {}): MockRoutes {
  return {
    'GET /api/agents/types/drafts': { data: DRAFTS },
    'GET /api/agents/types/usage': { data: USAGE },
    'GET /api/agents/types': { data: AGENT_TYPES },
    'GET /api/agents/runs': { data: RAW_RUNS },
    ...overrides,
  };
}

/** The agents list as an operator sees it, with mock agents and runs. */
const meta: Meta<typeof AgentsShell> = {
  title: 'Pages/Agents',
  component: AgentsShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/agents' } },
  },
};

export default meta;
type Story = StoryObj<typeof AgentsShell>;

// Decorators stack across meta and story, so each story installs its own frame once.
const withRoutes = (routes: MockRoutes) => [dashboardPage(routes, AgentsLayout)];

export const Default: Story = {
  decorators: withRoutes(listRoutes()),
  args: { initialAgentTypes: AGENT_ROWS, initialRuns: RUN_ROWS, initialError: null },
};

/** The table layout behind the list toggle. */
export const ListView: Story = {
  ...Default,
  play: async ({ canvasElement }) => {
    const toggle = await within(canvasElement).findByRole('button', { name: 'List view' });
    await userEvent.click(toggle);
  },
};

const emptyRoutes = listRoutes({
  'GET /api/agents/types/drafts': { data: [] },
  'GET /api/agents/types/usage': { data: [] },
  'GET /api/agents/types': { data: [] },
  'GET /api/agents/runs': { data: [] },
});

/** A new workspace: no agents, no runs, no drafts. */
export const Empty: Story = {
  args: { initialAgentTypes: [], initialRuns: [], initialError: null },
  decorators: withRoutes(emptyRoutes),
};

/** The server could not load agents. The page shows the error in the table, so this opens the list view. */
export const ErrorState: Story = {
  decorators: withRoutes(emptyRoutes),
  play: ListView.play,
  args: {
    initialAgentTypes: [],
    initialRuns: [],
    initialError: 'Failed to load agents (503). The agents service is not reachable.',
  },
};

const manyRoutes = listRoutes({
  'GET /api/agents/types': { data: MANY_AGENT_TYPES },
  'GET /api/agents/runs': { data: MANY_RAW_RUNS },
});

/** Twenty-plus agents, to check the grid and table at scale. */
export const ManyAgents: Story = {
  args: { initialAgentTypes: MANY_AGENT_ROWS, initialRuns: MANY_RUN_ROWS, initialError: null },
  decorators: withRoutes(manyRoutes),
};

export const ManyAgentsListView: Story = {
  ...ManyAgents,
  play: ListView.play,
};
