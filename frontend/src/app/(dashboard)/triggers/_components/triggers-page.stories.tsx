import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, waitFor, within } from 'storybook/test';
import type { TriggerRoutingCandidate } from '@runkora/shared/api';
import { dashboardPage } from '@runkora/storybook/dashboard-frame';
import type { MockRequest, MockRoutes } from '@runkora/storybook/mock-api';
import { TriggersShell } from './triggers-shell';
import {
  AGENTS,
  DELIVERIES,
  FAILED_DELIVERY,
  GITHUB_EVENT_TYPES,
  MANY_DELIVERIES,
  MANY_TRIGGERS,
  PULL_REQUEST_FIELDS,
  ROUTED_TRIGGER,
  SCHEDULE_PREVIEW,
  SOURCES,
  TRIGGERS,
  modelCatalog,
  routedDelivery,
  routingCandidates,
} from './triggers-mock-data';
import type { RoutingCase } from './triggers-mock-data';

/** The client-side calls the drawer and the wizard make while open. */
const catalogRoutes: MockRoutes = {
  'GET /api/triggers/routing/candidates': { data: routingCandidates(4) },
  'GET /api/models': { data: modelCatalog(true) },
  'GET /api/trigger-sources/:id/event-types': { data: GITHUB_EVENT_TYPES },
  'GET /api/trigger-providers/:provider/event-types': { data: GITHUB_EVENT_TYPES },
  'GET /api/trigger-providers/:provider/fields': { data: { fields: PULL_REQUEST_FIELDS } },
  'POST /api/triggers/schedule-preview': { data: SCHEDULE_PREVIEW },
  'POST /api/triggers': (req: MockRequest) => ({
    data: { id: 'trg_created01', ...(req.body as object) },
  }),
};

/** The Triggers page as an operator sees it, with a mock fleet and delivery history. */
const meta: Meta<typeof TriggersShell> = {
  title: 'Pages/Triggers',
  component: TriggersShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/triggers' } },
  },
  args: {
    sources: SOURCES,
    triggers: TRIGGERS,
    deliveries: DELIVERIES,
    agents: AGENTS,
    initialError: null,
  },
};

export default meta;
type Story = StoryObj<typeof TriggersShell>;

// Decorators stack across meta and story, so each story installs its own frame once.
const frame = [dashboardPage(catalogRoutes)];

/** A busy fleet: healthy, degraded and failing triggers of every kind, with a day of deliveries. */
export const Default: Story = { decorators: frame };

/** A new workspace with nothing configured. */
export const Empty: Story = {
  decorators: frame,
  args: { sources: [], triggers: [], deliveries: [] },
};

/** The server could not load triggers; the page shows the error over empty lists. */
export const LoadError: Story = {
  decorators: frame,
  args: {
    sources: [],
    triggers: [],
    deliveries: [],
    agents: [],
    initialError: 'Failed to load triggers (503). The triggers service is not reachable.',
  },
};

/** Thirty-odd triggers and sixty deliveries, to check pagination and density. */
export const ManyTriggers: Story = {
  decorators: frame,
  args: { triggers: MANY_TRIGGERS, deliveries: MANY_DELIVERIES },
};

/** The Deliveries tab, where the feed of arrivals and outcomes lives. */
export const DeliveriesTab: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole('tab', { name: 'Deliveries' }));
  },
};

/** An existing webhook trigger open in its drawer, with several filters and a long field path. */
export const TriggerDrawerOpen: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByText('review-opened-prs'));
  },
};

/** The drawer for a trigger the platform paused after sustained failure. */
export const TriggerDrawerAutoPaused: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByText('queue-health-check'));
  },
};

/** The drawer for a scheduled trigger. */
export const TriggerDrawerScheduled: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByText('nightly-dependency-audit'));
  },
};

/** A failed delivery open in the detail panel, with its wrapped error chain. */
export const DeliveryDetailOpen: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByRole('tab', { name: 'Deliveries' }));
    const rows = await canvas.findAllByText(/failed to start run/);
    const row = rows[0];
    if (row === undefined) throw new Error(`no row for ${FAILED_DELIVERY.id}`);
    await userEvent.click(row);
  },
};

/** The new-trigger wizard on its first step, choosing what starts the run. */
export const WizardOpen: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await userEvent.click(
      await within(canvasElement).findByRole('button', { name: /New trigger/ }),
    );
  },
};

/** A schedule trigger taken through every step to the review screen. */
export const WizardReview: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await userEvent.click(
      await within(canvasElement).findByRole('button', { name: /New trigger/ }),
    );
    const body = within(document.body);
    await userEvent.click(await body.findByRole('radio', { name: /Schedule/ }));
    await userEvent.type(await body.findByLabelText(/Trigger name/), 'daily-standup-digest');
    await userEvent.type(
      await body.findByLabelText('Prompt'),
      'Summarise yesterday’s merged pull requests and post a digest.',
    );
    const next = async () => {
      const button = await body.findByRole('button', { name: /Continue/ });
      await waitFor(() => {
        if ((button as HTMLButtonElement).disabled) throw new Error('Continue is disabled');
      });
      await userEvent.click(button);
    };
    await next();
    await userEvent.click(await body.findByRole('radio', { name: 'Code Reviewer' }));
    await next();
    await next();
  },
};

/** The webhook kind's first step, where the source, event type and filters are chosen. */
export const WizardWebhook: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await userEvent.click(
      await within(canvasElement).findByRole('button', { name: /New trigger/ }),
    );
    await userEvent.click(await within(document.body).findByRole('radio', { name: /Webhook/ }));
  },
};

// ── Let kora choose ──────────────────────────────────────────────────────────

/** The page with the routing step's answers swapped in. */
function routingFrame(routes: MockRoutes = {}) {
  return [dashboardPage({ ...catalogRoutes, ...routes })];
}

const withCandidates = (candidates: TriggerRoutingCandidate[]) => ({
  'GET /api/triggers/routing/candidates': { data: candidates },
});

/** Takes a webhook trigger through its first step to Target. */
async function openTargetStep(canvasElement: HTMLElement) {
  await userEvent.click(await within(canvasElement).findByRole('button', { name: /New trigger/ }));
  const body = within(document.body);
  await userEvent.click(await body.findByRole('radio', { name: /Webhook/ }));
  await userEvent.type(await body.findByLabelText(/Trigger name/), 'route-github-work');
  await userEvent.click(await body.findByRole('button', { name: 'Event type' }));
  const options = await body.findAllByText('issue_comment');
  const option = options[options.length - 1];
  if (option === undefined) throw new Error('no issue_comment event');
  await userEvent.click(option);
  await userEvent.type(
    await body.findByLabelText('Prompt'),
    'Handle this comment on {{{{ issue.html_url }}.',
  );
  await next(body);
  return body;
}

async function next(body: ReturnType<typeof within>) {
  const button = await body.findByRole('button', { name: /Continue/ });
  await waitFor(() => {
    if ((button as HTMLButtonElement).disabled) throw new Error('Continue is disabled');
  });
  await userEvent.click(button);
}

/** Switches the Target step to Let kora choose and picks the named candidates and a model. */
async function chooseKora(
  body: ReturnType<typeof within>,
  candidates: string[],
  {
    model = 'Liquid d1',
    fallback = candidates[0],
  }: { model?: string | null; fallback?: string } = {},
) {
  await userEvent.click(await body.findByRole('radio', { name: /Let kora choose/ }));
  for (const name of candidates) {
    await userEvent.click(await body.findByRole('checkbox', { name }));
  }
  if (model !== null) await userEvent.click(await body.findByRole('radio', { name: model }));
  if (fallback !== undefined && candidates.length > 0) {
    await userEvent.selectOptions(await body.findByLabelText('Fallback agent'), fallback);
  }
}

/** Brings the candidate list back into view after the settings below it took focus. */
async function showCandidates(body: ReturnType<typeof within>) {
  (await body.findByText('Candidate agents')).scrollIntoView({ block: 'start' });
}

/** Route to one agent: the step as it was, under the new mode choice. */
export const WizardTargetOneAgent: Story = {
  decorators: routingFrame(),
  play: async ({ canvasElement }) => {
    const body = await openTargetStep(canvasElement);
    await userEvent.click(await body.findByRole('radio', { name: 'Code Reviewer' }));
  },
};

/** Let kora choose between three candidates with a decision model picked. */
export const WizardKoraChooses: Story = {
  decorators: routingFrame(),
  play: async ({ canvasElement }) => {
    const body = await openTargetStep(canvasElement);
    await chooseKora(body, ['Code Reviewer', 'Coder', 'Security Reviewer']);
    await userEvent.type(
      await body.findByLabelText('Join key'),
      '{{{{ repository.full_name }}#{{{{ issue.number }}',
    );
  },
};

/** Twelve runnable candidates: the list searches and pages, the count tracks the selection. */
export const WizardManyCandidates: Story = {
  decorators: routingFrame(withCandidates(routingCandidates(12))),
  play: async ({ canvasElement }) => {
    const body = await openTargetStep(canvasElement);
    await chooseKora(body, ['Code Reviewer', 'Coder', 'Security Reviewer', 'Docs Writer']);
    await showCandidates(body);
  },
};

/** A candidate with no description, and one too short to route on. */
export const WizardMissingDescription: Story = {
  decorators: routingFrame(withCandidates(routingCandidates(5, { coder: '' }))),
  play: async ({ canvasElement }) => {
    const body = await openTargetStep(canvasElement);
    await chooseKora(body, ['Code Reviewer', 'Coder', 'Docs Writer']);
    await showCandidates(body);
  },
};

/** Nothing picked yet: Continue stays disabled until two agents are chosen. */
export const WizardEmptySelection: Story = {
  decorators: routingFrame(),
  play: async ({ canvasElement }) => {
    const body = await openTargetStep(canvasElement);
    await chooseKora(body, [], { model: null });
    await showCandidates(body);
  },
};

/** The catalog has no decision model, so this mode cannot be completed. */
export const WizardNoDecisionModels: Story = {
  decorators: routingFrame({ 'GET /api/models': { data: modelCatalog(false) } }),
  play: async ({ canvasElement }) => {
    const body = await openTargetStep(canvasElement);
    await userEvent.click(await body.findByRole('radio', { name: /Let kora choose/ }));
  },
};

/** A model search that matches nothing in the decision catalog. */
export const WizardModelSearchNoMatch: Story = {
  decorators: routingFrame(),
  play: async ({ canvasElement }) => {
    const body = await openTargetStep(canvasElement);
    await chooseKora(body, ['Code Reviewer', 'Coder'], { model: null });
    const input = await body.findByPlaceholderText('Search decision models');
    await userEvent.type(input, 'gpt');
    input.scrollIntoView({ block: 'center' });
  },
};

/** Create refused because the caller lacks the decision model's scope: the wizard names it. */
export const WizardMissingScopes: Story = {
  decorators: routingFrame({
    'POST /api/triggers': () =>
      new Response(
        JSON.stringify({
          code: 'forbidden',
          message: 'caller does not hold the scopes this routed trigger would be granted',
          details: { missing_scopes: ['inference:liquid:d1:use'] },
        }),
        { status: 403, headers: { 'Content-Type': 'application/json' } },
      ),
  }),
  play: async ({ canvasElement }) => {
    const body = await openTargetStep(canvasElement);
    await chooseKora(body, ['Code Reviewer', 'Security Reviewer']);
    await next(body);
    await next(body);
    await userEvent.click(await body.findByRole('button', { name: 'Create trigger' }));
    (await body.findByText(/Missing: inference:liquid:d1:use/)).scrollIntoView({
      block: 'center',
    });
  },
};

/** The routed trigger open in its drawer: the candidates instead of one agent. */
export const TriggerDrawerRouted: Story = {
  decorators: frame,
  args: { triggers: [ROUTED_TRIGGER, ...TRIGGERS] },
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByText('route-github-work'));
    const target = await within(document.body).findByText('kora chooses between 4 agents');
    target.scrollIntoView({ block: 'start' });
  },
};

/** The delivery panel for one routing outcome, opened from the Deliveries tab. */
function deliveryStory(c: RoutingCase): Story {
  return {
    decorators: frame,
    args: {
      triggers: [ROUTED_TRIGGER, ...TRIGGERS],
      deliveries: [routedDelivery(c), ...DELIVERIES],
    },
    play: async ({ canvasElement }) => {
      const canvas = within(canvasElement);
      await userEvent.click(await canvas.findByRole('tab', { name: 'Deliveries' }));
      const rows = await canvas.findAllByText('route-github-work');
      const row = rows[0];
      if (row === undefined) throw new Error('no routed delivery');
      await userEvent.click(row);
    },
  };
}

/** kora was sure: a new run of the chosen agent. */
export const DeliveryConfidentNewRun = deliveryStory('confident');

/** The join key matched an open run, so no model call was made. */
export const DeliveryJoinedByKey = deliveryStory('joined-by-key');

/** Below the confidence bar: the fallback agent took it. */
export const DeliveryLowConfidenceFallback = deliveryStory('fallback');

/** Below the confidence bar with no fallback: nothing ran. */
export const DeliveryLowConfidenceNoRun = deliveryStory('no-run');

/** The decision model did not answer: the fallback agent took it. */
export const DeliveryModelFailed = deliveryStory('model-failed');
