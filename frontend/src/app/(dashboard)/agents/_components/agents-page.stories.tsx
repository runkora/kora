import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import type { MockRequest, MockRoutes } from '@orbit/storybook/mock-api';
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
  SCALE_AGENT_ROWS,
  SCALE_AGENT_TYPES,
  SCALE_RUN_ROWS,
  SCALE_USAGE,
  RAW_RUNS,
  RUN_ROWS,
} from './agents-mock-data';
import { SCALE_BUDGETS, mockInsightsResponse } from './agents-insights-mock-data';
import type { Period } from '../_lib/spend';

/** The routes the list page calls on mount and on refresh, and the insights for each period. */
function listRoutes(overrides: MockRoutes = {}): MockRoutes {
  return {
    'GET /api/agents/types/drafts': { data: DRAFTS },
    'GET /api/agents/types': { data: AGENT_TYPES },
    'GET /api/agents/runs': { data: RAW_RUNS },
    'GET /api/agents/insights': ({ url }: MockRequest) =>
      mockInsightsResponse(url.searchParams.get('period') as Period),
    ...overrides,
  };
}

/**
 * The agents list as an operator sees it, with mock agents and runs. The 7-day insights
 * arrive as the server render's seed and the other periods through the mock API.
 */
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
  args: {
    initialAgentTypes: AGENT_ROWS,
    initialRuns: RUN_ROWS,
    initialError: null,
    initialInsights: mockInsightsResponse('7d'),
  },
};

/** The insights route fails: the figures that need it say they are unavailable. */
export const WithoutInsights: Story = {
  decorators: withRoutes(
    listRoutes({ 'GET /api/agents/insights': new Response(null, { status: 503 }) }),
  ),
  args: { initialAgentTypes: AGENT_ROWS, initialRuns: RUN_ROWS, initialError: null },
};

/** The page header's period control set to 30 days. */
export const ThirtyDays: Story = {
  ...Default,
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole('button', { name: '30d' }));
  },
};

/** The longest window, charted weekly. */
export const NinetyDays: Story = {
  ...Default,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByRole('button', { name: '90d' }));
    await userEvent.click(await canvas.findByRole('button', { name: 'Trend' }));
  },
};

/** The spend card's trend view. */
export const SpendTrend: Story = {
  ...Default,
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole('button', { name: 'Trend' }));
  },
};

/** The spend card's per-agent budgets view. */
export const SpendBudgets: Story = {
  ...Default,
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole('button', { name: 'Budgets' }));
  },
};

/** No runs in flight: the Running now card shows its empty line. */
export const NothingRunning: Story = {
  ...Default,
  args: {
    ...Default.args,
    initialRuns: RUN_ROWS.filter(
      (r) => !['active', 'completing', 'starting', 'pending'].includes(r.state),
    ),
  },
};

/** Two agents and no drafts. */
export const FewAgents: Story = {
  decorators: withRoutes(listRoutes({ 'GET /api/agents/types/drafts': { data: [] } })),
  args: {
    initialAgentTypes: AGENT_ROWS.slice(0, 2),
    initialRuns: RUN_ROWS.slice(0, 2),
    initialError: null,
    initialInsights: mockInsightsResponse('7d'),
  },
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
  args: {
    initialAgentTypes: MANY_AGENT_ROWS,
    initialRuns: MANY_RUN_ROWS,
    initialError: null,
    initialInsights: mockInsightsResponse('7d'),
  },
  decorators: withRoutes(manyRoutes),
};

export const ManyAgentsListView: Story = {
  ...ManyAgents,
  play: ListView.play,
};

const scaleInsights = (period: Period) =>
  mockInsightsResponse(period, SCALE_USAGE, SCALE_RUN_ROWS, SCALE_BUDGETS);

const scaleRoutes = listRoutes({
  'GET /api/agents/types': { data: SCALE_AGENT_TYPES },
  'GET /api/agents/insights': ({ url }: MockRequest) =>
    scaleInsights(url.searchParams.get('period') as Period),
});

/** 150 agents with spend, a budget on each and forty runs: the size a normal install reaches. */
export const HundredFiftyAgents: Story = {
  args: {
    initialAgentTypes: SCALE_AGENT_ROWS,
    initialRuns: SCALE_RUN_ROWS,
    initialError: null,
    initialInsights: scaleInsights('7d'),
  },
  decorators: withRoutes(scaleRoutes),
};
