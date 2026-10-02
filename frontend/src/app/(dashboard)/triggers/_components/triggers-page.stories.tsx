import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, waitFor, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import type { MockRequest, MockRoutes } from '@orbit/storybook/mock-api';
import TriggersLayout from '../layout';
import { TriggersShell } from './triggers-shell';
import {
  AGENTS,
  DELIVERIES,
  FAILED_DELIVERY,
  GITHUB_EVENT_TYPES,
  MANY_DELIVERIES,
  MANY_TRIGGERS,
  PULL_REQUEST_FIELDS,
  SCHEDULE_PREVIEW,
  SOURCES,
  TRIGGERS,
} from './triggers-mock-data';

/** The client-side calls the drawer and the wizard make while open. */
const catalogRoutes: MockRoutes = {
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
const frame = [dashboardPage(catalogRoutes, TriggersLayout)];

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
