import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import type { AgentOverviewResponse, AgentType } from '@runkora/shared/api';
import { AGENT_TYPES, CODE_REVIEWER } from '../../_components/agents-mock-data';
import type { Period } from '../../_lib/spend';
import { rowFor, runRowsFor, runsFor, withRoutes } from './agent-detail-page-mock-data';
import type { RunsData } from './agent-detail-page-mock-data';
import { LONG_INSTRUCTIONS, mockOverview, mockRunStatsOnly } from './agent-overview-mock-data';
import { AgentDetailShell } from './agent-detail-shell';

type OverviewFor = (slug: string, period: Period) => AgentOverviewResponse;

/**
 * The agent detail page: identity header plus Overview, Runs and Triggers, with a Configure modal.
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
    initialNextCursor: null,
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

/** An Overview story whose seed and period requests both answer from `overview`, for `agent`. */
const overviewStory = (
  overview: OverviewFor,
  agent: AgentType = CODE_REVIEWER,
  extra: Partial<Story> = {},
): Story => ({
  decorators: withRoutes(agent, overview),
  args: { initialAgent: rowFor(agent), initialOverview: overview(agent.slug, '7d') },
  ...extra,
});

// Decorators stack across meta and story, so each story installs its own frame once.
export const Overview: Story = overviewStory(mockOverview);

/** The 90 day window with one 2h 10m runaway run: the histogram stops at p99 and the run is the overflow bar. */
export const Overview90dOutlier: Story = overviewStory(
  (slug, period) => mockOverview(slug, period, { outlierMs: 130 * 60_000 }),
  CODE_REVIEWER,
  { play: openPeriod('90d') },
);

/** An agent with no budget: the Budget view is hidden. */
export const OverviewNoBudget: Story = overviewStory((slug, period) =>
  mockOverview(slug, period, { noBudget: true }),
);

const NO_RUNS: AgentType = { ...CODE_REVIEWER, slug: 'no-runs', name: 'No Runs' };
const SINGLE_RUN: AgentType = { ...CODE_REVIEWER, slug: 'single-run', name: 'Single Run' };

/** An agent with no runs and no usage: a dash for the median and a flat axis. */
export const OverviewNoRuns: Story = {
  ...overviewStory(mockOverview, NO_RUNS),
  args: {
    initialAgent: rowFor(NO_RUNS),
    initialRuns: [],
    initialOverview: mockOverview('no-runs', '7d'),
  },
};

/** An agent with a single finished run. */
export const OverviewSingleRun: Story = overviewStory(mockOverview, SINGLE_RUN);

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

/** The header's Configure button opens the builder modal at Identity. */
export const Configure: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole('button', { name: 'Configure' }));
  },
};

/** Opens Configure, then a rail section. The modal renders in a portal, so it is queried from the body. */
const configureSection =
  (section?: string): NonNullable<Story['play']> =>
  async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole('button', { name: 'Configure' }));
    if (!section) return;
    const body = within(canvasElement.ownerDocument.body);
    const dialog = await body.findByRole('dialog');
    await userEvent.click(await within(dialog).findByRole('button', { name: new RegExp(section) }));
  };

export const ConfigureBehaviour: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  play: configureSection('Behaviour'),
};

export const ConfigureTools: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  play: configureSection('Tools & access'),
};

export const ConfigureNetwork: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  play: configureSection('Network access'),
};

export const ConfigureGuardrails: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  play: configureSection('Guardrails'),
};

export const ConfigureDangerZone: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  play: configureSection('Danger zone'),
};

/** A caller without edit access: every field is read only. */
export const ConfigureReadOnly: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  args: { initialAgent: { ...rowFor(CODE_REVIEWER), canEdit: false } },
  play: configureSection('Behaviour'),
};

/** An agent declared in git: read only, with the managed-in-git notice. */
export const ConfigureManagedInGit: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  args: { initialAgent: { ...rowFor(CODE_REVIEWER), declared: true } },
  play: configureSection(),
};

/** The Edit link on the Instructions card opens the Configure modal at Behaviour. */
export const EditInstructions: Story = {
  ...Overview,
  play: async ({ canvasElement }) => {
    await userEvent.click(
      await within(canvasElement).findByRole('button', { name: 'Edit instructions' }),
    );
  },
};

/** The Overview's Recent Runs: View all runs switches to the Runs tab. */
export const OverviewViewAllRuns: Story = {
  ...Overview,
  play: async ({ canvasElement }) => {
    await userEvent.click(
      await within(canvasElement).findByRole('button', { name: /View all runs/ }),
    );
  },
};

/** The Runs tab seeded and served with one run data set. */
const runsStory = (set: RunsData): Story => ({
  decorators: withRoutes(CODE_REVIEWER, mockOverview, runsFor(CODE_REVIEWER, set)),
  args: { initialRuns: runRowsFor(CODE_REVIEWER, set) },
  play: openTab('Runs'),
});

/** Runs from pull requests, Slack, an alert, a schedule and a person, each with its source chips and starter. */
export const Runs: Story = runsStory('default');

/** Every summary far longer than the row: the title truncates, chips and time keep their place. */
export const RunsLongTitles: Story = runsStory('long');

/** Seven links on the newest run: two delivery chips, the other five behind "+5". */
export const RunsManySources: Story = runsStory('many');

/** No run has a source: the rows end with the starter and time. */
export const RunsNoSources: Story = runsStory('none');

export const RunsEmpty: Story = runsStory('empty');

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
