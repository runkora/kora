import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage } from '@runkora/storybook/dashboard-frame';
import { TriggersShell } from './triggers-shell';
import { AGENTS, DELIVERIES, SOURCES, TRIGGERS } from './triggers-mock-data';
import {
  firstIds,
  modelCatalog,
  routedDelivery,
  routingAgents,
} from './triggers-mock-data-proposed';
import type { RoutingCase } from './triggers-mock-data-proposed';
import { TargetStepWizardProposed } from './trigger-wizard/target-step-wizard-proposed';
import { DeliveryDetailPanelProposed } from './delivery-routing-proposed';

/**
 * Proposed: a trigger can let kora choose the agent for each delivery. The
 * wizard's Target step gains the mode, and the delivery panel shows how each
 * delivery was routed.
 */
const meta: Meta<typeof TargetStepWizardProposed> = {
  title: 'Pages/Triggers (proposed)',
  component: TargetStepWizardProposed,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/triggers' } },
  },
  args: { kind: 'webhook', sources: SOURCES, agents: routingAgents(4) },
};

export default meta;
type Story = StoryObj<typeof TargetStepWizardProposed>;

const withModels = [dashboardPage({ 'GET /api/models': { data: modelCatalog(true) } })];
const withoutModels = [dashboardPage({ 'GET /api/models': { data: modelCatalog(false) } })];

const three = routingAgents(4);
const many = routingAgents(10);
const thin = routingAgents(4, { coder: '' });
const choose = (agents: typeof three, n: number) => ({
  mode: 'choose' as const,
  candidateIds: firstIds(agents, n),
  modelId: 'liquid/d1',
  fallbackId: firstIds(agents, 1)[0] ?? '',
});

/** Route to one agent: today's step, unchanged, under the new mode choice. */
export const WizardOneAgent: Story = {
  decorators: withModels,
  args: { agentTypeId: AGENTS[0]?.id ?? '' },
};

/** Let kora choose between three candidates with a decision model picked. */
export const WizardKoraChooses: Story = {
  decorators: withModels,
  args: { routing: choose(three, 3) },
};

/** Twelve candidates: the list searches and pages, the count tracks the selection. */
export const WizardManyCandidates: Story = {
  decorators: withModels,
  args: { agents: many, routing: choose(many, 4) },
};

/** A candidate with no description, and one too short to route on. */
export const WizardMissingDescription: Story = {
  decorators: withModels,
  args: { agents: thin, routing: { ...choose(thin, 3), candidateIds: firstIds(thin, 4) } },
};

/** Nothing picked yet: Continue stays disabled until two agents are chosen. */
export const WizardEmptySelection: Story = {
  decorators: withModels,
  args: { routing: { mode: 'choose' } },
};

/** The catalog has no decision model, so this mode cannot be completed. */
export const WizardNoDecisionModels: Story = {
  decorators: withoutModels,
  args: { routing: { ...choose(three, 3), modelId: '' } },
};

/** A model search that matches nothing in the decision catalog. */
export const WizardModelSearchNoMatch: Story = {
  decorators: withModels,
  args: { routing: { ...choose(three, 3), modelId: '' } },
  play: async () => {
    const input = await within(document.body).findByPlaceholderText('Search decision models');
    await userEvent.type(input, 'gpt');
    input.scrollIntoView({ block: 'center' });
  },
};

/** The delivery panel over the Triggers page, for one routing outcome. */
function deliveryStory(c: RoutingCase): Story {
  const { delivery, decision } = routedDelivery(c);
  return {
    decorators: withModels,
    render: () => (
      <>
        <TriggersShell
          sources={SOURCES}
          triggers={TRIGGERS}
          deliveries={DELIVERIES}
          agents={AGENTS}
          initialError={null}
        />
        <DeliveryDetailPanelProposed
          delivery={delivery}
          decision={decision}
          onClose={() => undefined}
        />
      </>
    ),
  };
}

/** kora was sure: a new run of the chosen agent. */
export const DeliveryConfidentNewRun = deliveryStory('confident');

/** The coalesce key matched an open run, so no model call was made. */
export const DeliveryJoinedByKey = deliveryStory('joined-by-key');

/** Below the confidence bar: the fallback agent took it. */
export const DeliveryLowConfidenceFallback = deliveryStory('fallback');

/** Below the confidence bar with no fallback: nothing ran. */
export const DeliveryLowConfidenceNoRun = deliveryStory('no-run');

/** The decision model did not answer: the fallback agent took it. */
export const DeliveryModelFailed = deliveryStory('model-failed');
