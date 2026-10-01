import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import type {
  AgentType,
  AgentTypeUsage,
  EffectiveAccess,
  Trigger,
  TriggerDelivery,
  WebhookSource,
} from '@orbit/shared/api';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import type { MockRoutes } from '@orbit/storybook/mock-api';
import AgentsLayout from '../../layout';
import {
  AGENT_TYPES,
  CODE_REVIEWER,
  CONNECTIONS,
  ENVIRONMENTS,
  MCP_CATALOG,
  MCP_SERVERS,
  MODELS_CATALOGUE,
  RAW_RUNS,
  USAGE,
  ago,
  runsForAgent,
} from '../../_components/agents-mock-data';
import { indexAgentTypes, toAgentTypeRow, toRunRow } from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';
import type { BudgetPrincipalStatus, TenantCapStatus } from '../../../model-gateway/_types';
import { AgentDetailShell } from './agent-detail-shell';

const DAY = 24 * 60 * 60 * 1000;

// ── Mock data specific to the detail page ────────────────────────────────────

const BUDGET: BudgetPrincipalStatus = {
  principal_id: 'spiffe://kora.example/tenant/ten_8f3a21/agent/code-reviewer',
  principal_type: 'AGENT',
  categories: [
    {
      source: 'grant',
      id: 'grant_code_reviewer',
      limit_usd: 150,
      reset_cadence: 'monthly',
      per_run_limit_usd: 4,
      spend_usd: 96.4,
    },
  ],
  effective_limit_usd: 150,
  effective_reset_cadence: 'monthly',
  effective_spend_usd: 96.4,
  current_spend_usd: 96.4,
  has_limit: true,
  spend_limit_reached: false,
  last_active_at: ago(4 * 60_000),
};

const TENANT_CAP: TenantCapStatus = {
  limit_usd: 2000,
  reset_cadence: 'monthly',
  spend_usd: 812.55,
  reached: false,
};

const SOURCES: WebhookSource[] = [
  {
    id: 'src_github',
    tenant_id: 'ten_8f3a21',
    name: 'kora/platform on GitHub',
    provider: 'github',
    enabled: true,
    created_at: ago(30 * DAY),
    updated_at: ago(30 * DAY),
  },
];

const TRIGGERS: Trigger[] = [
  {
    id: 'trg_pr_opened',
    tenant_id: 'ten_8f3a21',
    kind: 'webhook',
    source_id: 'src_github',
    name: 'github pull_request.opened',
    agent_type_id: CODE_REVIEWER.id,
    agent_slug: CODE_REVIEWER.slug,
    event_type: 'pull_request.opened',
    match_condition: { 'repository.full_name': ['kora/platform'] },
    prompt_template: 'Review pull request {{pull_request.number}} in {{repository.full_name}}.',
    enabled: true,
    created_at: ago(28 * DAY),
    updated_at: ago(10 * DAY),
    max_parallel: 4,
    overflow: 'wait',
    max_queue_depth: 32,
    timeout_seconds: 30,
    dedupe_enabled: true,
    autopause_enabled: true,
    consecutive_failures: 0,
    pending_count: 0,
  },
  {
    id: 'trg_weekly_digest',
    tenant_id: 'ten_8f3a21',
    kind: 'scheduled',
    name: 'weekly review digest',
    agent_type_id: CODE_REVIEWER.id,
    agent_slug: CODE_REVIEWER.slug,
    prompt_template: 'Summarise the pull requests reviewed this week.',
    schedule_config: { cron_expression: '0 9 * * 1', timezone: 'Australia/Sydney' },
    enabled: true,
    created_at: ago(14 * DAY),
    updated_at: ago(14 * DAY),
    max_parallel: 1,
    overflow: 'refuse',
    max_queue_depth: 0,
    timeout_seconds: 30,
    dedupe_enabled: false,
    autopause_enabled: true,
    consecutive_failures: 0,
    pending_count: 0,
  },
];

const DELIVERIES: TriggerDelivery[] = [];

function effectiveAccess(agent: AgentType): EffectiveAccess {
  const env = ENVIRONMENTS.find((e) => e.id === agent.environment_id);
  const own = agent.connectivity.hosts ?? [];
  const envHosts = env?.egress_hosts ?? [];
  const connectorEgress = (agent.connectors ?? []).map((c) => ({
    connection: c.connection,
    adapter: c.connection,
    hosts: c.connection === 'github' ? ['api.github.com'] : ['slack.com'],
  }));
  return {
    open: agent.connectivity.mode === 'open',
    own_hosts: own,
    ...(env ? { environment_id: env.id, environment_name: env.name } : {}),
    environment_hosts: envHosts,
    connector_egress: connectorEgress,
    connectors: agent.connectors ?? [],
    hosts: [...new Set([...own, ...envHosts, ...connectorEgress.flatMap((c) => c.hosts)])].sort(),
  };
}

/** Usage for an agent that has not run yet. */
const noUsage = (agent: AgentType): AgentTypeUsage => ({
  agent_slug: agent.slug,
  prompt_tokens: 0,
  completion_tokens: 0,
  cache_read_tokens: 0,
  cache_creation_tokens: 0,
  total_tokens: 0,
  estimated_cost_usd: 0,
});

/** Every route the detail page and its four tabs call. */
function detailRoutes(agent: AgentType): MockRoutes {
  return {
    'GET /api/agents/types/usage/:slug':
      USAGE.find((u) => u.agent_slug === agent.slug) ?? noUsage(agent),
    'GET /api/agents/types/:id/effective-access': { data: effectiveAccess(agent) },
    'POST /api/agents/types/effective-access': { data: effectiveAccess(agent) },
    'GET /api/agents/types/:id/runs': { data: runsForAgent(agent.id) },
    'GET /api/agents/types/:id': { data: agent },
    'GET /api/models': { data: MODELS_CATALOGUE },
    'GET /api/agents/connectors': { data: CONNECTIONS },
    'GET /api/agents/mcp-tools': { servers: MCP_SERVERS, catalog: MCP_CATALOG },
    'GET /api/agents/environments': { data: ENVIRONMENTS },
    'GET /api/triggers': { data: TRIGGERS.filter((t) => t.agent_type_id === agent.id) },
    'GET /api/trigger-sources': { data: SOURCES },
    'GET /api/trigger-deliveries': { data: DELIVERIES },
  };
}

const withRoutes = (agent: AgentType) => [dashboardPage(detailRoutes(agent), AgentsLayout)];

const rowFor = (agent: AgentType): AgentTypeRow => toAgentTypeRow(agent);

const runRowsFor = (agent: AgentType): RunRow[] => {
  const byId = indexAgentTypes([rowFor(agent)]);
  return RAW_RUNS.filter((r) => r.agent_type_id === agent.id).map((r) => toRunRow(r, byId));
};

// ── Stories ──────────────────────────────────────────────────────────────────

/** The agent detail page: identity header plus Overview, Runs, Configuration and Triggers. */
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
    budget: BUDGET,
    tenantCap: TENANT_CAP,
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

// Decorators stack across meta and story, so each story installs its own frame once.
export const Overview: Story = { decorators: withRoutes(CODE_REVIEWER) };

export const Configuration: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  play: openTab('Configuration'),
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
    budget: null,
    tenantCap: null,
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
    budget: null,
    tenantCap: null,
  },
  decorators: withRoutes(PROVISIONING),
  parameters: {
    nextjs: { appDirectory: true, navigation: { pathname: '/agents/incident-scribe' } },
  },
};

export const NotFound: Story = {
  decorators: withRoutes(CODE_REVIEWER),
  args: { initialAgent: null, initialRuns: [], initialStatus: 'not_found', budget: null },
};
