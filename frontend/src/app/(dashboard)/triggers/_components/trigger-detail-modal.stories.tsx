import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage, sessionRoutes } from '@runkora/storybook/dashboard-frame';
import { TriggersShell } from './triggers-shell';
import { catalogRoutes } from './triggers-story-routes';
import {
  AGENTS,
  PERMUTATION_DELIVERIES,
  PERMUTATION_TRIGGERS,
  SOURCES,
} from './triggers-mock-data';

/**
 * One story per layout the trigger detail modal takes.
 *
 * The modal changes shape along three axes (see the permutations in
 * `triggers-mock-data.ts`): the trigger's kind, whether it runs one agent or
 * lets kora choose, and a state such as auto-paused or managed in git. Each
 * story is named `<Trigger><Tab>[<Section>]`, where the trigger is the fixture
 * it opens (`WebhookAgent` opens `webhook-agent`). Overview stories cover every
 * fixture, with its target and delivery history; Configuration stories cover the
 * sections where the fixtures differ, the Danger zone last.
 */
const meta: Meta<typeof TriggersShell> = {
  title: 'Pages/Trigger detail modal',
  component: TriggersShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/triggers' } },
  },
  args: {
    sources: SOURCES,
    triggers: PERMUTATION_TRIGGERS,
    deliveries: PERMUTATION_DELIVERIES,
    agents: AGENTS,
    initialError: null,
  },
  decorators: [dashboardPage(catalogRoutes)],
};

export default meta;
type Story = StoryObj<typeof TriggersShell>;

/** The Slack source before its signing secret is saved, for the waiting-for-secret fixture. */
const WAITING_SOURCES = SOURCES.map((s) =>
  s.provider === 'slack' ? { ...s, signingSecretSet: false } : s,
);

const body = () => within(document.body);

type Tab = 'Overview' | 'Configuration';

/** Opens the named trigger's modal, then optionally a tab and a Configuration section. */
function openTrigger(name: string, tab?: Tab, section?: string): Story['play'] {
  return async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByText(name));
    if (tab !== undefined) await userEvent.click(await body().findByRole('tab', { name: tab }));
    if (section !== undefined)
      await userEvent.click(await body().findByRole('button', { name: new RegExp(`^${section}`) }));
  };
}

// ── Overview: the layout follows the kind, with the state laid over it ──────

/** A webhook trigger: the endpoint to paste into the provider, its target and its deliveries. */
export const WebhookAgentOverview: Story = { play: openTrigger('webhook-agent') };

/** A chat command: the same, labelled as the request URL. */
export const ChatAgentOverview: Story = { play: openTrigger('chat-agent') };

/** kora chooses between candidates, so the Target shows the routing instead of one agent. */
export const WebhookRoutedOverview: Story = { play: openTrigger('webhook-routed') };

/** A routed chat command joins a Slack thread to its open run by key. */
export const ChatRoutedOverview: Story = { play: openTrigger('chat-routed') };

/** A scheduled trigger has no endpoint and no secret, only the stats. */
export const ScheduledAgentOverview: Story = { play: openTrigger('scheduled-agent') };

/** A manual trigger shows the form it publishes and the way to run it. */
export const ManualAgentOverview: Story = { play: openTrigger('manual-agent') };

/** The platform disabled the trigger after repeated failures, and says so. */
export const ScheduledAgentAutoPausedOverview: Story = {
  play: openTrigger('scheduled-agent-auto-paused'),
};

/** The source was deleted: no endpoint, and the trigger can only be turned off or deleted. */
export const WebhookAgentSourceDeletedOverview: Story = {
  play: openTrigger('webhook-agent-source-deleted'),
};

/** Declared in the configuration repository: read-only, under the managed-in-git notice. */
export const WebhookAgentManagedInGitOverview: Story = {
  play: openTrigger('webhook-agent-managed-in-git'),
};

// ── Configuration: the sections follow the wizard's steps ───────────────────

/** Name is editable; the source is fixed at creation and labelled Read-only. */
export const WebhookAgentConfigurationTrigger: Story = {
  play: openTrigger('webhook-agent', 'Configuration', 'Trigger'),
};

/** The event, and the filters that narrow which of its deliveries start a run. */
export const WebhookAgentConfigurationEventFilters: Story = {
  play: openTrigger('webhook-agent', 'Configuration', 'Event filters'),
};

/** No filters: every delivery of the event starts a run. */
export const WebhookAgentUnfilteredConfigurationEventFilters: Story = {
  play: openTrigger('webhook-agent-unfiltered', 'Configuration', 'Event filters'),
};

/**
 * Everything the Senders step can show: the help line that mentions roles, the allowed-senders
 * field and its hint, a card per role the provider reports (two ticked, one not), and, once a
 * role is changed, the unsaved-changes marks and the Save and Discard row.
 */
export const WebhookAgentConfigurationSenders: Story = {
  play: async (context) => {
    await openTrigger('webhook-agent', 'Configuration', 'Senders')?.(context);
    await userEvent.click(await body().findByRole('checkbox', { name: 'Collaborator' }));
  },
};

/** A chat command on Slack: sender ids only, since Slack reports no roles. */
export const ChatAgentConfigurationSenders: Story = {
  play: openTrigger('chat-agent', 'Configuration', 'Senders'),
};

/** Capacity, overflow, timeout, coalescing and safety, saved together. */
export const WebhookAgentConfigurationDelivery: Story = {
  play: openTrigger('webhook-agent', 'Configuration', 'Delivery'),
};

/** Choosing Refuse on a webhook: the unsaved-changes dot on the rail, the refuse summary, no queue fields. */
export const WebhookAgentConfigurationDeliveryRefuse: Story = {
  play: async (ctx) => {
    await openTrigger('webhook-agent', 'Configuration', 'Delivery')?.(ctx);
    await userEvent.click(await body().findByRole('radio', { name: 'Refuse' }));
  },
};

/** Out-of-range values: the message under each field, the summary withheld, the count beside Save. */
export const WebhookAgentConfigurationDeliveryInvalid: Story = {
  play: async (ctx) => {
    await openTrigger('webhook-agent', 'Configuration', 'Delivery')?.(ctx);
    const user = userEvent;
    const set = async (label: string, value: string) => {
      const input = await body().findByLabelText(label);
      await user.clear(input);
      if (value !== '') await user.type(input, value);
    };
    await set('Queue size', '600');
    const coalesce = await body().findByRole('switch', { name: /Coalesce events/ });
    if (coalesce.getAttribute('aria-checked') !== 'true') await user.click(coalesce);
    await set('Key', '');
    await set('Window', '600');
    await set('Time to start', '2');
  },
};

/** A manual trigger: Wait is recommended and there is no coalescing. */
export const ManualAgentConfigurationDelivery: Story = {
  play: openTrigger('manual-agent', 'Configuration', 'Delivery'),
};

/** Refuse on a manual trigger: the summary promises the 429 the caller will see. */
export const ManualAgentConfigurationDeliveryRefuse: Story = {
  play: async (ctx) => {
    await openTrigger('manual-agent', 'Configuration', 'Delivery')?.(ctx);
    await userEvent.click(await body().findByRole('radio', { name: 'Refuse' }));
  },
};

/** Without triggers:write the controls are dead and the footer says why. */
export const WebhookAgentConfigurationDeliveryViewOnly: Story = {
  decorators: [dashboardPage({ ...catalogRoutes, ...sessionRoutes(['triggers:read']) })],
  play: openTrigger('webhook-agent', 'Configuration', 'Delivery'),
};

/** A chat command offers its events as cards instead of a searchable list. */
export const ChatAgentConfigurationEventFilters: Story = {
  play: openTrigger('chat-agent', 'Configuration', 'Event filters'),
};

/** The cron expression, in words, with its timezone and catch-up. */
export const ScheduledAgentConfigurationSchedule: Story = {
  play: openTrigger('scheduled-agent', 'Configuration', 'Schedule'),
};

/** Weekly: day buttons and a time, with the timezone and the scheduler's next runs in the summary. */
export const ScheduledWeeklyConfigurationSchedule: Story = {
  play: openTrigger('scheduled-weekly', 'Configuration', 'Schedule'),
};

/** Minutes: the interval, with the minutes past the hour it lands on. */
export const ScheduledMinutesConfigurationSchedule: Story = {
  play: openTrigger('scheduled-minutes', 'Configuration', 'Schedule'),
};

/** Monthly: a day of the month and a time. */
export const ScheduledMonthlyConfigurationSchedule: Story = {
  play: openTrigger('scheduled-monthly', 'Configuration', 'Schedule'),
};

/** A saved expression the builder cannot show opens on Custom expression, with its parts spelled out; the Cron builder card warns that going back replaces it. */
export const ScheduledCustomConfigurationSchedule: Story = {
  play: openTrigger('scheduled-custom', 'Configuration', 'Schedule'),
};

/** An out-of-range field is marked in the parts and named under the input; Save is blocked only for a malformed expression. */
export const ScheduledCustomInvalidConfigurationSchedule: Story = {
  play: openTrigger('scheduled-custom-invalid', 'Configuration', 'Schedule'),
};

/** Daily, switched to Hourly to see the other frequency's field. */
export const ScheduledAgentConfigurationScheduleHourly: Story = {
  play: async (ctx) => {
    await openTrigger('scheduled-agent', 'Configuration', 'Schedule')?.(ctx);
    await userEvent.click(await body().findByRole('radio', { name: 'Hourly' }));
  },
};

/** No coalescing: that only exists for deliveries that arrive from outside. */
export const ScheduledAgentConfigurationDelivery: Story = {
  play: openTrigger('scheduled-agent', 'Configuration', 'Delivery'),
};

/** A manual trigger has only a name to set here; there is no source. */
export const ManualAgentConfigurationTrigger: Story = {
  play: openTrigger('manual-agent', 'Configuration', 'Trigger'),
};

/** Every control disabled under the managed-in-git notice. */
export const WebhookAgentManagedInGitConfigurationEventFilters: Story = {
  play: openTrigger('webhook-agent-managed-in-git', 'Configuration', 'Event filters'),
};

/** The Trigger section of a trigger managed in git: the notice leads, every control is dimmed. */
export const WebhookAgentManagedInGitConfigurationTrigger: Story = {
  play: openTrigger('webhook-agent-managed-in-git', 'Configuration', 'Trigger'),
};

/** The Senders section of a trigger managed in git: the notice leads, every control is dimmed. */
export const WebhookAgentManagedInGitConfigurationSenders: Story = {
  play: openTrigger('webhook-agent-managed-in-git', 'Configuration', 'Senders'),
};

/** The Prompt section of a trigger managed in git: the notice leads, every control is dimmed. */
export const WebhookAgentManagedInGitConfigurationPrompt: Story = {
  play: openTrigger('webhook-agent-managed-in-git', 'Configuration', 'Prompt'),
};

/** The Delivery section of a trigger managed in git: the notice leads, every control is dimmed. */
export const WebhookAgentManagedInGitConfigurationDelivery: Story = {
  play: openTrigger('webhook-agent-managed-in-git', 'Configuration', 'Delivery'),
};

/** The Danger zone section of a trigger managed in git: the notice leads, every control is dimmed. */
export const WebhookAgentManagedInGitConfigurationDangerZone: Story = {
  play: openTrigger('webhook-agent-managed-in-git', 'Configuration', 'Danger zone'),
};

// ── Danger zone: the last Configuration section ─────────────────────────────

/** Rotating the signing secret, in amber, and deleting the trigger, in red. */
export const WebhookAgentConfigurationDangerZone: Story = {
  play: openTrigger('webhook-agent', 'Configuration', 'Danger zone'),
};

/** A secret the platform issues is replaced by pasting the new one, never generated. */
export const ChatAgentConfigurationDangerZone: Story = {
  play: openTrigger('chat-agent', 'Configuration', 'Danger zone'),
};

/** The first secret is set, not replaced. */
export const ChatAgentWaitingForSecretConfigurationDangerZone: Story = {
  args: { sources: WAITING_SOURCES },
  play: openTrigger('chat-agent-waiting-for-secret', 'Configuration', 'Danger zone'),
};

/** No secret to manage: only delete. */
export const ScheduledAgentConfigurationDangerZone: Story = {
  play: openTrigger('scheduled-agent', 'Configuration', 'Danger zone'),
};
