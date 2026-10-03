/**
 * Mock kora workspace for the Agents page stories. Data is authored in the raw
 * API shapes and run through the real transformers, so the stories exercise the
 * same mapping the pages do. Timestamps are relative to load time, which keeps
 * "2 hours ago" style copy realistic whenever a story is opened.
 */

import type {
  AgentType,
  AgentTypeDraft,
  AgentTypeUsage,
  ConnectionCatalogEntry,
  Environment,
  McpCatalogTool,
  McpServer,
  ModelObject,
  Run,
  RunWithAttribution,
} from '@orbit/shared/api';
import { indexAgentTypes, toAgentTypeRow, toRunRow } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** ISO timestamp for a moment `ms` before now. */
export const ago = (ms: number): string => new Date(Date.now() - ms).toISOString();

/** ISO timestamp for a moment `ms` from now. */
export const ahead = (ms: number): string => new Date(Date.now() + ms).toISOString();

const TENANT = 'ten_8f3a21';

// ── Agent types ──────────────────────────────────────────────────────────────

const GITHUB_REVIEW = { connection: 'github', actions: ['contents:read', 'pull_requests:write'] };

function agentType(
  partial: Partial<AgentType> & Pick<AgentType, 'id' | 'name' | 'slug' | 'description'>,
): AgentType {
  return {
    tenant_id: TENANT,
    system_prompt: 'You are a careful engineering assistant working inside the kora workspace.',
    framework: 'claude-code',
    identity_mode: 'autonomous',
    grant_scopes: ['inference:invoke'],
    connectivity: { mode: 'none' },
    connectors: [],
    mcp_tools: [],
    max_concurrent_runs_per_pod: 10,
    guardrails: {},
    state: 'active',
    created_at: ago(21 * DAY),
    updated_at: ago(2 * DAY),
    model_id: 'anthropic/claude-sonnet-4-6',
    environment_id: null,
    ...partial,
  };
}

export const CODE_REVIEWER: AgentType = agentType({
  id: 'at_code_reviewer',
  name: 'Code Reviewer',
  slug: 'code-reviewer',
  description: 'Reviews pull requests, leaves inline comments and flags risky changes.',
  system_prompt: [
    'You review pull requests for the platform team.',
    'Read the diff and the surrounding code before commenting.',
    'Leave inline comments only where you can name a concrete problem and a fix.',
    'Summarise the review in one comment: what changed, what worries you, what is fine.',
    'Never approve a change that touches migrations or auth without a human reviewer.',
  ].join('\n'),
  grant_scopes: ['inference:invoke', 'mcp:github:pull_requests.get:use'],
  connectivity: { mode: 'whitelist', hosts: ['api.github.com', 'github.com'] },
  connectors: [GITHUB_REVIEW],
  mcp_tools: ['mcp:github:pull_requests.get:use', 'mcp:github:pull_requests.comment:use'],
  guardrails: { repeated_call_warn: 5, repeated_call_pause: 10 },
  created_at: ago(34 * DAY),
  updated_at: ago(3 * DAY),
  environment_id: 'env_engineering',
  budget: { limit_usd: 150, reset_cadence: 'monthly', per_run_limit_usd: 4 },
});

export const AGENT_TYPES: AgentType[] = [
  CODE_REVIEWER,
  agentType({
    id: 'at_coder',
    name: 'Coder',
    slug: 'coder',
    description: 'A general purpose coding agent that picks up tickets and opens pull requests.',
    framework: 'codex',
    model_id: 'openai/gpt-5-codex',
    connectivity: { mode: 'whitelist', hosts: ['registry.npmjs.org', 'pkg.go.dev'] },
    connectors: [
      {
        connection: 'github',
        actions: ['contents:read', 'contents:write', 'pull_requests:write'],
        resources: ['kora/platform', 'kora/dashboard'],
      },
    ],
    mcp_tools: ['mcp:linear:issues.get:use', 'mcp:linear:issues.update:use'],
    max_concurrent_runs_per_pod: 6,
    budget: { limit_usd: 400, reset_cadence: 'weekly', per_run_limit_usd: 12 },
    created_at: ago(40 * DAY),
    updated_at: ago(1 * DAY),
  }),
  agentType({
    id: 'at_security_reviewer',
    name: 'Security Reviewer',
    slug: 'security-reviewer',
    description: 'Scans dependency and code changes for vulnerabilities and unsafe patterns.',
    model_id: 'anthropic/claude-opus-4-6',
    connectivity: { mode: 'whitelist', hosts: ['api.github.com', 'osv.dev', 'nvd.nist.gov'] },
    connectors: [{ connection: 'github', actions: ['contents:read', 'security_events:read'] }],
    mcp_tools: ['mcp:github:pull_requests.get:use'],
    guardrails: { repeated_call_warn: 3, repeated_call_pause: 6, wait_rearm_pause: 8 },
    created_at: ago(18 * DAY),
    updated_at: ago(5 * DAY),
  }),
  agentType({
    id: 'at_release_notes',
    name: 'Release Notes',
    slug: 'release-notes',
    description: 'Drafts release notes from merged pull requests and posts them to Slack.',
    framework: 'opencode',
    model_id: 'anthropic/claude-haiku-4-5',
    identity_mode: 'delegated',
    connectors: [
      { connection: 'github', actions: ['contents:read'] },
      { connection: 'slack', actions: ['chat:write', 'channels:read'] },
    ],
    created_at: ago(12 * DAY),
    updated_at: ago(12 * DAY),
  }),
  agentType({
    id: 'at_triage_bot',
    name: 'Triage Bot',
    slug: 'triage-bot',
    description: 'Labels and routes new issues, and asks for missing reproduction steps.',
    model_id: 'anthropic/claude-haiku-4-5',
    connectivity: { mode: 'open' },
    mcp_tools: ['mcp:linear:issues.get:use', 'mcp:linear:issues.update:use'],
    connectors: [{ connection: 'github', actions: ['issues:read', 'issues:write'] }],
    max_concurrent_runs_per_pod: 20,
    budget: { limit_usd: 60, reset_cadence: 'monthly' },
    created_at: ago(9 * DAY),
    updated_at: ago(1 * DAY),
  }),
  agentType({
    id: 'at_incident_scribe',
    name: 'Incident Scribe',
    slug: 'incident-scribe',
    description: 'Builds an incident timeline from alerts and chat, then drafts the postmortem.',
    identity_mode: 'delegated',
    state: 'provisioning',
    created_at: ago(2 * MINUTE),
    updated_at: ago(2 * MINUTE),
  }),
];

const EXTRA_NAMES: Array<[string, string]> = [
  [
    'Dependency Updater',
    'Bumps dependencies and runs the test suite before opening a pull request.',
  ],
  ['Docs Writer', 'Keeps the reference docs in step with merged API changes.'],
  ['Migration Checker', 'Reviews database migrations for locks and unsafe rewrites.'],
  ['Flaky Test Hunter', 'Finds tests that fail intermittently and proposes fixes.'],
  ['Changelog Curator', 'Groups merged changes into customer-facing changelog entries.'],
  ['Onboarding Guide', 'Answers new engineer questions from the internal handbook.'],
  ['Cost Watcher', 'Reports on inference spend and flags agents trending over budget.'],
  ['Accessibility Auditor', 'Audits dashboard pages against WCAG and files issues.'],
  ['Support Summariser', 'Summarises long support threads for the on-call engineer.'],
  ['API Linter', 'Checks OpenAPI changes against the kora API style guide.'],
  ['Perf Profiler', 'Profiles slow endpoints and suggests query and cache fixes.'],
  ['Standup Digest', 'Posts a daily summary of merged work and open blockers.'],
  ['Compliance Mapper', 'Maps control evidence to the audit checklist each quarter.'],
  ['Refactor Planner', 'Plans large refactors as a sequence of small, reviewable changes.'],
  ['Spec Reviewer', 'Reviews product specs for gaps before engineering picks them up.'],
];

const FRAMEWORKS: AgentType['framework'][] = ['claude-code', 'codex', 'opencode'];
const MODELS = [
  'anthropic/claude-sonnet-4-6',
  'anthropic/claude-haiku-4-5',
  'openai/gpt-5-codex',
  'anthropic/claude-opus-4-6',
];

const slugify = (name: string): string => name.toLowerCase().replace(/[^a-z0-9]+/g, '-');

/** A workspace with a long agent list, for checking the grid and table at scale. */
export const MANY_AGENT_TYPES: AgentType[] = [
  ...AGENT_TYPES,
  ...EXTRA_NAMES.map(([name, description], i) =>
    agentType({
      id: `at_extra_${i}`,
      name,
      slug: slugify(name),
      description,
      framework: FRAMEWORKS[i % FRAMEWORKS.length] ?? 'claude-code',
      model_id: MODELS[i % MODELS.length] ?? 'anthropic/claude-sonnet-4-6',
      identity_mode: i % 4 === 0 ? 'delegated' : 'autonomous',
      state: i === 7 ? 'deleting' : 'active',
      created_at: ago((3 + i * 2) * DAY),
      updated_at: ago((1 + i) * DAY),
    }),
  ),
];

// ── Runs ─────────────────────────────────────────────────────────────────────

const OPERATOR = { id: 'usr_42b7e9', name: 'Dev Operator' };
const PRIYA = { id: 'usr_71c0aa', name: 'Priya Raman' };
const MARCUS = { id: 'usr_19de03', name: 'Marcus Webb' };

interface RunSeed {
  agent: AgentType;
  state: Run['state'];
  /** Start time, ms before now. */
  startedAgo: number;
  /** Run length in ms; omit for runs still going or never started. */
  lasted?: number;
  by?: { id: string; name: string };
  summary?: string | null;
  toolCalls?: number;
  trigger?: Run['started_by'];
}

let runCounter = 0;

function run(seed: RunSeed): RunWithAttribution {
  runCounter += 1;
  const by = seed.by ?? OPERATOR;
  const started = ago(seed.startedAgo);
  return {
    id: `run_${String(runCounter).padStart(4, '0')}`,
    tenant_id: TENANT,
    agent_type_id: seed.agent.id,
    state: seed.state,
    pod_ordinal: runCounter % 3,
    created_by: by.id,
    created_by_name: by.name,
    started_at: seed.state === 'pending' ? null : started,
    completed_at:
      seed.lasted !== undefined
        ? new Date(Date.now() - seed.startedAgo + seed.lasted).toISOString()
        : null,
    created_at: started,
    updated_at: started,
    ...(seed.toolCalls !== undefined ? { tool_calls: seed.toolCalls } : {}),
    ...(seed.summary !== undefined ? { summary: seed.summary } : {}),
    ...(seed.trigger ? { started_by: seed.trigger } : {}),
  };
}

const PR_OPENED = {
  trigger_id: 'trg_pr_opened',
  trigger_name: 'github pull_request.opened',
  delivery_id: 'dlv_5b21',
} as const;
const NIGHTLY = { trigger_id: 'trg_nightly', trigger_name: 'nightly dependency scan' } as const;
const NEW_ISSUE = {
  trigger_id: 'trg_issue_opened',
  trigger_name: 'github issues.opened',
  delivery_id: 'dlv_9ac4',
} as const;

const [REVIEWER, CODER, SECURITY, NOTES, TRIAGE, SCRIBE] = AGENT_TYPES as [
  AgentType,
  AgentType,
  AgentType,
  AgentType,
  AgentType,
  AgentType,
];

/** Runs across every state, newest first, as the runs endpoint returns them. */
export const RAW_RUNS: RunWithAttribution[] = [
  run({
    agent: REVIEWER,
    state: 'active',
    startedAgo: 4 * MINUTE,
    by: OPERATOR,
    toolCalls: 9,
    summary: 'Reviewing pull request 412, which moves session storage to Redis.',
    trigger: PR_OPENED,
  }),
  run({
    agent: CODER,
    state: 'active',
    startedAgo: 17 * MINUTE,
    by: PRIYA,
    toolCalls: 31,
    summary: 'Implementing KOR-318: paginate the audit log endpoint.',
  }),
  run({ agent: TRIAGE, state: 'starting', startedAgo: 40_000, by: OPERATOR, trigger: NEW_ISSUE }),
  run({ agent: SECURITY, state: 'pending', startedAgo: 20_000, by: OPERATOR }),
  run({
    agent: NOTES,
    state: 'idle',
    startedAgo: 52 * MINUTE,
    by: MARCUS,
    toolCalls: 14,
    summary: 'Waiting for a human to confirm the 2.14 release notes before posting to Slack.',
  }),
  run({
    agent: REVIEWER,
    state: 'completing',
    startedAgo: 31 * MINUTE,
    lasted: 29 * MINUTE,
    toolCalls: 22,
    summary: 'Reviewed pull request 409 and left 6 inline comments.',
    trigger: PR_OPENED,
  }),
  run({
    agent: REVIEWER,
    state: 'completed',
    startedAgo: 1 * HOUR + 10 * MINUTE,
    lasted: 6 * MINUTE + 12_000,
    toolCalls: 18,
    summary: 'Reviewed pull request 407; approved with two nits.',
    trigger: PR_OPENED,
  }),
  run({
    agent: REVIEWER,
    state: 'completed',
    startedAgo: 3 * HOUR,
    lasted: 4 * MINUTE + 41_000,
    toolCalls: 15,
    summary: 'Reviewed pull request 405 and requested changes on the retry logic.',
    trigger: PR_OPENED,
  }),
  run({
    agent: CODER,
    state: 'completed',
    startedAgo: 2 * HOUR,
    lasted: 21 * MINUTE + 5_000,
    by: PRIYA,
    toolCalls: 64,
    summary: 'Fixed the flaky session timeout test and opened pull request 411.',
  }),
  run({
    agent: TRIAGE,
    state: 'completed',
    startedAgo: 2 * HOUR + 20 * MINUTE,
    lasted: 48_000,
    toolCalls: 6,
    summary: 'Labelled issue 233 as bug, area/dashboard and asked for browser details.',
    trigger: NEW_ISSUE,
  }),
  run({
    agent: TRIAGE,
    state: 'completed',
    startedAgo: 5 * HOUR,
    lasted: 39_000,
    toolCalls: 5,
    summary: 'Routed issue 231 to the billing team.',
    trigger: NEW_ISSUE,
  }),
  run({
    agent: SECURITY,
    state: 'completed',
    startedAgo: 7 * HOUR,
    lasted: 12 * MINUTE + 30_000,
    toolCalls: 27,
    summary: 'Scanned 142 dependencies; found one high severity advisory in a transitive package.',
    trigger: NIGHTLY,
  }),
  run({
    agent: SECURITY,
    state: 'failed',
    startedAgo: 1 * DAY + 7 * HOUR,
    lasted: 2 * MINUTE + 3_000,
    toolCalls: 3,
    summary: 'Stopped: the advisory database returned 429 three times in a row.',
    trigger: NIGHTLY,
  }),
  run({
    agent: CODER,
    state: 'budget_exhausted',
    startedAgo: 1 * DAY + 2 * HOUR,
    lasted: 38 * MINUTE,
    by: MARCUS,
    toolCalls: 112,
    summary:
      'Refactoring the policy bundle compiler; hit the 12 USD per-run limit before finishing.',
  }),
  run({
    agent: NOTES,
    state: 'completed',
    startedAgo: 1 * DAY + 3 * HOUR,
    lasted: 3 * MINUTE + 18_000,
    by: PRIYA,
    toolCalls: 11,
    summary: 'Drafted the 2.13 release notes from 38 merged pull requests.',
  }),
  run({
    agent: REVIEWER,
    state: 'failed',
    startedAgo: 2 * DAY,
    lasted: 51_000,
    toolCalls: 2,
    summary: 'Could not fetch pull request 398: the GitHub connection token was revoked.',
    trigger: PR_OPENED,
  }),
  run({
    agent: CODER,
    state: 'completed',
    startedAgo: 2 * DAY + 4 * HOUR,
    lasted: 14 * MINUTE,
    by: OPERATOR,
    toolCalls: 41,
    summary: 'Added request id propagation to the identity service logs.',
  }),
  run({
    agent: TRIAGE,
    state: 'completed',
    startedAgo: 3 * DAY,
    lasted: 44_000,
    toolCalls: 7,
    summary: 'Closed issue 220 as a duplicate of 198.',
    trigger: NEW_ISSUE,
  }),
  run({
    agent: SCRIBE,
    state: 'failed',
    startedAgo: 4 * DAY,
    lasted: 9_000,
    toolCalls: 0,
    summary: null,
  }),
];

export const AGENT_ROWS: AgentTypeRow[] = AGENT_TYPES.map(toAgentTypeRow);
export const MANY_AGENT_ROWS: AgentTypeRow[] = MANY_AGENT_TYPES.map(toAgentTypeRow);

export const RUN_ROWS: RunRow[] = RAW_RUNS.map((r) => toRunRow(r, indexAgentTypes(AGENT_ROWS)));

/** Runs for a larger workspace: the base runs plus a spread across the extra agents. */
export const MANY_RAW_RUNS: RunWithAttribution[] = [
  ...RAW_RUNS,
  ...MANY_AGENT_TYPES.slice(AGENT_TYPES.length, AGENT_TYPES.length + 8).map((a, i) =>
    run({
      agent: a,
      state: i % 3 === 0 ? 'active' : 'completed',
      startedAgo: (i + 2) * 50 * MINUTE,
      ...(i % 3 === 0 ? {} : { lasted: (3 + i) * MINUTE }),
      toolCalls: 8 + i * 3,
      summary: `Routine run for ${a.name}.`,
    }),
  ),
];

export const MANY_RUN_ROWS: RunRow[] = MANY_RAW_RUNS.map((r) =>
  toRunRow(r, indexAgentTypes(MANY_AGENT_ROWS)),
);

/** Raw runs of one agent, as the per-agent runs endpoint returns them. */
export const runsForAgent = (agentId: string): RunWithAttribution[] =>
  RAW_RUNS.filter((r) => r.agent_type_id === agentId);

// ── Usage ────────────────────────────────────────────────────────────────────

const usage = (
  slug: string,
  prompt: number,
  completion: number,
  cacheRead: number,
  cacheWrite: number,
  cost: number,
): AgentTypeUsage => ({
  agent_slug: slug,
  prompt_tokens: prompt,
  completion_tokens: completion,
  cache_read_tokens: cacheRead,
  cache_creation_tokens: cacheWrite,
  total_tokens: prompt + completion + cacheRead + cacheWrite,
  estimated_cost_usd: cost,
});

export const USAGE: AgentTypeUsage[] = [
  usage('code-reviewer', 1_420_000, 318_000, 4_900_000, 610_000, 38.42),
  usage('coder', 3_870_000, 902_000, 11_300_000, 1_240_000, 112.07),
  usage('security-reviewer', 640_000, 141_000, 1_800_000, 220_000, 21.9),
  usage('release-notes', 210_000, 64_000, 380_000, 52_000, 1.84),
  usage('triage-bot', 480_000, 76_000, 910_000, 98_000, 3.15),
];

// ── Drafts ───────────────────────────────────────────────────────────────────

export const DRAFTS: AgentTypeDraft[] = [
  {
    id: 'drf_01',
    tenant_id: TENANT,
    created_by: OPERATOR.id,
    payload: { name: 'Docs Writer', slug: 'docs-writer', framework: 'claude-code' },
    created_at: ago(2 * DAY),
    updated_at: ago(3 * HOUR),
  },
];

// ── Configuration catalogues ─────────────────────────────────────────────────

const model = (id: string, display: string, owner: string, family: string): ModelObject => ({
  id,
  object: 'model',
  created: 1_760_000_000,
  owned_by: owner,
  family,
  display_name: display,
  status: 'available',
  aliases: [],
  input_modalities: ['text', 'image'],
  output_modalities: ['text'],
  dialects: ['anthropic', 'openai'],
  capabilities: ['chat'],
});

export const MODELS_CATALOGUE: ModelObject[] = [
  model('anthropic/claude-sonnet-4-6', 'Claude Sonnet 4.6', 'bedrock', 'anthropic'),
  model('anthropic/claude-opus-4-6', 'Claude Opus 4.6', 'bedrock', 'anthropic'),
  model('anthropic/claude-haiku-4-5', 'Claude Haiku 4.5', 'bedrock', 'anthropic'),
  model('openai/gpt-5-codex', 'GPT-5 Codex', 'azure', 'openai'),
];

export const CONNECTIONS: ConnectionCatalogEntry[] = [
  {
    name: 'github',
    adapter: 'github',
    actions: [
      'contents:read',
      'contents:write',
      'issues:read',
      'issues:write',
      'pull_requests:write',
      'security_events:read',
    ],
    baseline: ['metadata:read'],
    qualifier: { name: 'repository', description: 'Limit access to specific repositories.' },
  },
  {
    name: 'slack',
    adapter: 'slack',
    actions: ['chat:write', 'channels:read', 'users:read'],
    baseline: [],
    qualifier: null,
  },
];

export const MCP_SERVERS: McpServer[] = [
  {
    name: 'github',
    display_name: 'GitHub',
    description: 'Pull requests, issues and repository contents.',
    transport: 'streamable-http',
    caller_identity: 'delegated',
    endpoint: 'https://mcp.github.example/v1',
    global: true,
    tool_count: 4,
    domains: ['pull_requests', 'issues'],
    last_synced: ago(2 * HOUR),
    status: 'healthy',
  },
  {
    name: 'linear',
    display_name: 'Linear',
    description: 'Tickets, projects and cycles.',
    transport: 'streamable-http',
    caller_identity: 'delegated',
    endpoint: 'https://mcp.linear.example/v1',
    global: false,
    tool_count: 3,
    domains: ['issues'],
    last_synced: ago(5 * HOUR),
    status: 'healthy',
  },
];

const tool = (server: string, name: string, description: string): McpCatalogTool => ({
  tenant_id: TENANT,
  server_slug: server,
  tool_name: name,
  scope: `mcp:${server}:${name}:use`,
  description,
});

export const MCP_CATALOG: McpCatalogTool[] = [
  tool('github', 'pull_requests.get', 'Fetch a pull request with its diff.'),
  tool('github', 'pull_requests.comment', 'Comment on a pull request.'),
  tool('github', 'issues.get', 'Fetch an issue and its comments.'),
  tool('github', 'issues.label', 'Add or remove labels on an issue.'),
  tool('linear', 'issues.get', 'Fetch a ticket.'),
  tool('linear', 'issues.update', 'Update a ticket status or assignee.'),
  tool('linear', 'issues.create', 'Create a ticket.'),
];

export const ENVIRONMENTS: Environment[] = [
  {
    id: 'env_engineering',
    tenant_id: TENANT,
    name: 'Engineering',
    description: 'Package registries and the internal docs site.',
    egress_hosts: ['registry.npmjs.org', 'pkg.go.dev', 'docs.kora.example'],
    connectors: [{ connection: 'slack', actions: ['channels:read'] }],
    created_at: ago(60 * DAY),
    updated_at: ago(20 * DAY),
  },
];

// ── A workspace with 150 agents ──────────────────────────────────────────────

/** How many agents the scale stories carry. A normal install has more than a hundred. */
export const SCALE_AGENT_COUNT = 150;

/** 150 generated agents, for checking every card and table at the size a normal install reaches. */
export const SCALE_AGENT_TYPES: AgentType[] = Array.from({ length: SCALE_AGENT_COUNT }, (_, i) => {
  const name = `Agent ${String(i + 1).padStart(3, '0')}`;
  return agentType({
    id: `at_scale_${i}`,
    name,
    slug: slugify(name),
    description: `Generated agent ${i + 1} for the scale stories.`,
    framework: FRAMEWORKS[i % FRAMEWORKS.length] ?? 'claude-code',
    model_id: MODELS[i % MODELS.length] ?? 'anthropic/claude-sonnet-4-6',
    identity_mode: i % 5 === 0 ? 'delegated' : 'autonomous',
    created_at: ago((3 + i) * DAY),
    updated_at: ago((1 + (i % 9)) * DAY),
  });
});

export const SCALE_AGENT_ROWS: AgentTypeRow[] = SCALE_AGENT_TYPES.map(toAgentTypeRow);

/** Forty live and finished runs spread over the generated agents. */
export const SCALE_RUN_ROWS: RunRow[] = SCALE_AGENT_TYPES.slice(0, 40).map((a, i) =>
  toRunRow(
    run({
      agent: a,
      state: i % 4 === 3 ? 'completed' : i % 4 === 2 ? 'starting' : 'active',
      startedAgo: (i + 1) * 7 * MINUTE,
      ...(i % 4 === 3 ? { lasted: 5 * MINUTE } : {}),
      toolCalls: 4 + i,
    }),
    indexAgentTypes(SCALE_AGENT_ROWS),
  ),
);

/** Usage for every generated agent, falling off steeply so a few agents dominate the spend. */
export const SCALE_USAGE: AgentTypeUsage[] = SCALE_AGENT_TYPES.map((a, i) => {
  const weight = 1 / (1 + i * 0.35);
  return usage(
    a.slug,
    Math.round(2_000_000 * weight),
    Math.round(400_000 * weight),
    Math.round(6_000_000 * weight),
    Math.round(500_000 * weight),
    Math.round(120 * weight * 100) / 100,
  );
});
