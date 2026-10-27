import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, waitFor, within } from 'storybook/test';
import type { TriggerRoutingCandidate } from '@runkora/shared/api';
import { dashboardPage } from '@runkora/storybook/dashboard-frame';
import type { MockRequest, MockRoutes } from '@runkora/storybook/mock-api';
import { TriggersShell } from './triggers-shell';
import {
  AGENTS,
  CHAT_FIELDS,
  DELIVERIES,
  EVENT_TYPES_BY_PROVIDER,
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

/** The path segment after `after`, e.g. the provider in `/api/trigger-providers/slack/fields`. */
function segmentAfter(req: MockRequest, after: string): string {
  const parts = req.url.pathname.split('/');
  return parts[parts.indexOf(after) + 1] ?? '';
}

/** Each provider answers with its own catalog, so chat and webhook kinds each see their own. */
function providerEventTypes(provider: string) {
  return { data: EVENT_TYPES_BY_PROVIDER[provider] ?? GITHUB_EVENT_TYPES };
}

/** The client-side calls the drawer and the wizard make while open. */
const catalogRoutes: MockRoutes = {
  'GET /api/triggers/routing/candidates': { data: routingCandidates(4) },
  'GET /api/models': { data: modelCatalog(true) },
  'GET /api/trigger-sources/:id/event-types': (req: MockRequest) => {
    const id = segmentAfter(req, 'trigger-sources');
    return providerEventTypes(SOURCES.find((s) => s.id === id)?.provider ?? 'github');
  },
  'GET /api/trigger-providers/:provider/event-types': (req: MockRequest) =>
    providerEventTypes(segmentAfter(req, 'trigger-providers')),
  'GET /api/trigger-providers/:provider/fields': (req: MockRequest) => {
    const provider = segmentAfter(req, 'trigger-providers');
    const fields =
      EVENT_TYPES_BY_PROVIDER[provider]?.category === 'chat' ? CHAT_FIELDS : PULL_REQUEST_FIELDS;
    // A generic source has only the meta fields.
    return {
      data: {
        fields: provider === 'generic' ? fields.filter((f) => f.path.startsWith('meta.')) : fields,
      },
    };
  },
  'POST /api/triggers/schedule-preview': { data: SCHEDULE_PREVIEW },
  'POST /api/triggers': (req: MockRequest) => ({
    data: { id: 'trg_created01', ...(req.body as object) },
  }),
  // A provider that issues its own secret gets none back; the others get a generated one.
  'POST /api/trigger-sources': (req: MockRequest) => {
    const body = req.body as { name: string; provider: string };
    const chat = EVENT_TYPES_BY_PROVIDER[body.provider]?.category === 'chat';
    return {
      data: {
        id: 'src_created01',
        name: body.name,
        provider: body.provider,
        category: chat ? 'chat' : 'webhook',
        enabled: true,
        webhook_path: '/api/v1/webhooks/src_created01',
        ...(chat ? {} : { secret: 'whsec_9f2c41d8e7a0b3' }),
      },
    };
  },
  'PATCH /api/trigger-sources/:id': (req: MockRequest) => ({
    data: { id: segmentAfter(req, 'trigger-sources'), webhook_path: '/x' },
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

/** The mock fleet with its Slack source still waiting for the signing secret. */
const WAITING_SOURCES = SOURCES.map((s) =>
  s.provider === 'slack' ? { ...s, signingSecretSet: false } : s,
);

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

/** A new generic webhook source, verified by HMAC with a secret the sender issued. */
export const WizardGenericSource: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await userEvent.click(
      await within(canvasElement).findByRole('button', { name: /New trigger/ }),
    );
    const body = within(document.body);
    await userEvent.click(await body.findByRole('radio', { name: /Webhook/ }));
    await userEvent.click(await body.findByText('New source'));
    await userEvent.click(await body.findByText('Generic webhook'));
    await userEvent.click(await body.findByText('HMAC-SHA256'));
    await userEvent.type(await body.findByLabelText(/Signature header/), 'X-Signature');
    await userEvent.click(await body.findByText("Paste the sender's secret"));
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

/** Takes a mention trigger to Let kora choose and types the join key. */
function chatRoutingStory(joinKey: string): Story {
  return {
    decorators: routingFrame(),
    play: async ({ canvasElement }) => {
      await openKind(canvasElement, /Chat command/);
      const body = within(document.body);
      await userEvent.type(await body.findByLabelText(/Trigger name/), 'answer-platform-questions');
      await userEvent.click(await body.findByRole('radio', { name: /Mention/ }));
      await userEvent.type(
        await body.findByLabelText('Prompt'),
        'Answer {{{{ text }} in the thread and link anything you cite.',
      );
      await next(body);
      await chooseKora(body, ['Code Reviewer', 'Coder', 'Security Reviewer']);
      const input = await body.findByLabelText('Join key');
      await userEvent.type(input, joinKey);
      input.scrollIntoView({ block: 'center' });
    },
  };
}

/** A chat command where kora chooses: the join key keeps a Slack thread on one run. */
export const WizardChatKoraChooses = chatRoutingStory('{{{{ conversation.id }}:{{{{ thread.id }}');

/** A join key naming a Slack field instead of a chat field: Continue stays disabled. */
export const WizardChatJoinKeyNotAField = chatRoutingStory('{{{{ event.thread_ts }}');

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

/** Opens the wizard and picks a kind from the first phase. */
async function openKind(canvasElement: HTMLElement, kind: RegExp) {
  // Disabled until the session's scopes load, which renders a new button.
  const button = await waitFor(() => {
    const b = within(canvasElement).getByRole('button', { name: /New trigger/ });
    if ((b as HTMLButtonElement).disabled) throw new Error('New trigger is disabled');
    return b;
  });
  await userEvent.click(button);
  await userEvent.click(await within(document.body).findByRole('radio', { name: kind }));
}

/** Names a new Slack source in the chat command wizard and creates it. */
async function createSlackSource(canvasElement: HTMLElement) {
  await openKind(canvasElement, /Chat command/);
  const body = within(document.body);
  await userEvent.type(await body.findByLabelText(/Trigger name/), 'answer-platform-questions');
  await userEvent.click(await body.findByText('New source'));
  await userEvent.type(await body.findByLabelText(/Source name/), 'acme-slack-ops');
  await userEvent.click(await body.findByRole('radio', { name: /Slack/ }));
  await userEvent.click(await body.findByRole('button', { name: 'Create source' }));
  await body.findByText('manifest.json');
  return body;
}

/** Pastes the signing secret Slack issued and saves it. */
async function saveSigningSecret(body: ReturnType<typeof within>) {
  await userEvent.type(
    await body.findByLabelText(/Signing secret/),
    '8f14e45fceea167a5a36dedd4bea2543',
  );
  await userEvent.click(await body.findByRole('button', { name: 'Save secret' }));
  await body.findByText('Signing secret saved.');
}

/** The chat command source step before the secret: the source exists, with the manifest, the request URL and the secret field. */
export const WizardChatCommand: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    const body = await createSlackSource(canvasElement);
    await body.findByLabelText(/Signing secret/);
    (await body.findByText('Slack app')).scrollIntoView({ block: 'start' });
  },
};

/** The chat command source step after the secret is saved: the wizard can continue. */
export const WizardChatCommandSecretSaved: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    const body = await createSlackSource(canvasElement);
    await saveSigningSecret(body);
    (await body.findByText('Slack app')).scrollIntoView({ block: 'start' });
  },
};

/** A chat command on the existing Slack source: the mention event, a channel filter and two senders. */
export const WizardChatEvent: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await openKind(canvasElement, /Chat command/);
    const body = within(document.body);
    await userEvent.type(await body.findByLabelText(/Trigger name/), 'answer-platform-questions');
    await userEvent.click(await body.findByRole('radio', { name: /Mention/ }));
    await userEvent.click(await body.findByRole('button', { name: /Add filter/ }));
    await userEvent.click(await body.findByRole('button', { name: 'Filter 1 field' }));
    await userEvent.click(await body.findByText('conversation.id'));
    await userEvent.type(await body.findByLabelText('Filter 1 values'), 'C04PLATFORM | C07ONCALL');
    await userEvent.type(await body.findByLabelText('Allowed senders'), 'U012AB3CD, U045EF6GH');
    (await body.findByText('Senders')).scrollIntoView({ block: 'center' });
  },
};

/** The done step for a new Slack source, set up before the trigger. */
export const WizardChatDone: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    const body = await createSlackSource(canvasElement);
    await saveSigningSecret(body);
    await userEvent.click(await body.findByRole('radio', { name: /Mention/ }));
    await userEvent.type(
      await body.findByLabelText('Prompt'),
      'Answer the question in the thread and link anything you cite.',
    );
    await next(body);
    await userEvent.click(await body.findByRole('radio', { name: 'Triage Bot' }));
    await next(body);
    await next(body);
    await userEvent.click(await body.findByRole('button', { name: /Create trigger/ }));
    await body.findByText('Trigger created');
  },
};

/** A chat command trigger in its drawer, replacing the Slack signing secret. */
export const DrawerChatTrigger: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByText('answer-platform-questions'));
    const body = within(document.body);
    await userEvent.type(
      await body.findByLabelText(/Signing secret/),
      'c9f0f895fb98ab9159f51fd0297e236d',
    );
    await userEvent.click(await body.findByRole('button', { name: 'Replace secret' }));
    await body.findByText(/The new secret is stored/);
  },
};

/** The triggers page with a chat source still waiting for its signing secret. */
export const ChatSourceWaitingForSecret: Story = {
  decorators: frame,
  args: { sources: WAITING_SOURCES },
  play: async ({ canvasElement }) => {
    await within(canvasElement).findAllByText('Waiting for signing secret');
  },
};

/** The drawer of a trigger on a waiting chat source, with the way to set its secret. */
export const DrawerChatTriggerWaiting: Story = {
  decorators: frame,
  args: { sources: WAITING_SOURCES },
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByText('answer-platform-questions'));
    const body = within(document.body);
    await userEvent.type(
      await body.findByLabelText(/Signing secret/),
      'c9f0f895fb98ab9159f51fd0297e236d',
    );
    await userEvent.click(await body.findByRole('button', { name: 'Set secret' }));
    await body.findByText(/The signing secret is stored/);
  },
};

/** The webhook kind on a GitHub source: role cards read from the labels GitHub declares. */
export const WizardWebhookSenders: Story = {
  decorators: frame,
  play: async ({ canvasElement }) => {
    await openKind(canvasElement, /Webhook/);
    const body = within(document.body);
    await body.findByText('Owns the repository');
    (await body.findByText('Senders')).scrollIntoView({ block: 'center' });
  },
};
