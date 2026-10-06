import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import type { AgentOverviewResponse } from '@orbit/shared/api';
import { AGENT_TYPES, CODE_REVIEWER } from '../../_components/agents-mock-data';
import type { Period } from '../../_lib/spend';
import { rowFor, runRowsFor, withRoutes } from './agent-detail-page-mock-data';
import { LONG_INSTRUCTIONS, mockOverview, mockRunStatsOnly } from './agent-overview-mock-data';
import { AgentDetailShell } from './agent-detail-shell';

type OverviewFor = (slug: string, period: Period) => AgentOverviewResponse;

/**
 * The agent detail page: identity header plus Overview, Runs, Configuration and Triggers.
 * The 7-day overview arrives as the server render's seed and the other periods through the mock API.
 */
const meta: Meta<typeof AgentDetailShell> = {
  title: 'Pages/Agents/Detail',
  component: AgentDetailShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/agents/code-reviewer' } },
  },
  args: {
    initialAgent: rowFor(CODE_REVIEWER),
    initialRuns: runRowsFor(CODE_REVIEWER),
    initialError: null,
    initialStatus: 'success',
    initialOverview: mockOverview(CODE_REVIEWER.slug, '7d'),
  },
};

export default meta;
type Story = StoryObj<typeof AgentDetailShell>;

/** Opens a tab by its visible name once the page has rendered. */
const openTab =
  (name: string): NonNullable<Story['play']> =>
  async ({ canvasElement }) => {
    const tab = await within(canvasElement).findByRole('tab', { name });
    await userEvent.click(tab);
  };

/** Picks a period in the header control. */
const openPeriod =
  (period: Period): NonNullable<Story['play']> =>
  async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole('button', { name: period }));
  };

/** An Overview story whose seed and period requests both answer from `overview`. */
const overviewStory = (overview: OverviewFor, extra: Partial<Story> = {}): Story => ({
  decorators: withRoutes(CODE_REVIEWER, overview),
  args: { initialOverview: overview(CODE_REVIEWER.slug, '7d') },
  ...extra,
});

// Decorators stack across meta and story, so each story installs its own frame once.
export const Overview: Story = overviewStory(mockOverview);

export const Overview24h: Story = overviewStory(mockOverview, { play: openPeriod('24h') });

export const Overview30d: Story = overviewStory(mockOverview, { play: openPeriod('30d') });

export const Overview90d: Story = overviewStory(mockOverview, { play: openPeriod('90d') });

/** The 90 day window with one 2h 10m runaway run: the histogram stops at p99 and the run is the overflow bar. */
export const Overview90dOutlier: Story = overviewStory(
  (slug, period) => mockOverview(slug, period, { outlierMs: 130 * 60_000 }),
  { play: openPeriod('90d') },
);

/** An agent with no budget: the Budget view is hidden. */
export const OverviewNoBudget: Story = overviewStory((slug, period) =>
  mockOverview(slug, period, { noBudget: true }),
);

/** An agent with no runs and no usage: a dash for the median and a flat axis. */
export const OverviewNoRuns: Story = {
  ...overviewStory((_, period) => mockOverview('no-runs', period)),
  args: { initialOverview: mockOverview('no-runs', '7d'), initialRuns: [] },
};

/** An agent with a single finished run. */
export const OverviewSingleRun: Story = overviewStory((_, period) =>
  mockOverview('single-run', period),
);

/** A caller without agents:usage:read: the tokens, spend, budget and token usage cards are absent. */
export const OverviewWithoutUsageScope: Story = overviewStory(mockRunStatsOnly);

/** A long markdown prompt: the Instructions card clamps it and offers Expand. */
export const OverviewLongInstructions: Story = {
  ...Overview,
  args: {
    ...Overview.args,
    initialAgent: { ...rowFor(CODE_REVIEWER), systemPrompt: LONG_INSTRUCTIONS },
  },
};

/** An agent with no instructions. */
export const OverviewNoInstructions: Story = {
  ...Overview,
  args: { ...Overview.args, initialAgent: { ...rowFor(CODE_REVIEWER), systemPrompt: '' } },
};

export const Configuration: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  play: openTab('Configuration'),
};

/** The Edit link on the Instructions card opens the Configuration tab at Behaviour. */
export const EditInstructions: Story = {
  ...Overview,
  play: async ({ canvasElement }) => {
    await userEvent.click(
      await within(canvasElement).findByRole('button', { name: 'Edit instructions' }),
    );
  },
};

export const Runs: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  play: openTab('Runs'),
};

export const Triggers: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  play: openTab('Triggers'),
};

const RELEASE_NOTES = AGENT_TYPES[3] ?? CODE_REVIEWER;

/** A delegated agent with no budget configured. */
export const DelegatedAgent: Story = {
  args: {
    initialAgent: rowFor(RELEASE_NOTES),
    initialRuns: runRowsFor(RELEASE_NOTES),
    initialOverview: mockOverview(RELEASE_NOTES.slug, '7d'),
  },
  decorators: withRoutes(RELEASE_NOTES),
  parameters: {
    nextjs: { appDirectory: true, navigation: { pathname: '/agents/release-notes' } },
  },
};

const PROVISIONING = AGENT_TYPES[5] ?? CODE_REVIEWER;

/** A freshly created agent that is still being provisioned. */
export const Provisioning: Story = {
  args: {
    initialAgent: rowFor(PROVISIONING),
    initialRuns: [],
    initialOverview: mockOverview(PROVISIONING.slug, '7d'),
  },
  decorators: withRoutes(PROVISIONING),
  parameters: {
    nextjs: { appDirectory: true, navigation: { pathname: '/agents/incident-scribe' } },
  },
};

export const NotFound: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  args: { initialAgent: null, initialRuns: [], initialStatus: 'not_found', initialOverview: null },
};
