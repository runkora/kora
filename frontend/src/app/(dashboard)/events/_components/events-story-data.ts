/**
 * @tier 3 — events-scoped story and test data. A tenant's last six hours of platform events in the
 * read API's envelope: runs and their sandboxes, a trigger whose run failed and the event
 * trigger that reacted to it, budgets running out, sign-ins and tokens, admin edits and a
 * declared resource that would not apply. No payload carries a token, secret or credential.
 */

import type { EventPrincipalType, KoraEvent } from '../_types';

const TENANT = 'kora-tenant-example';

const agentSubject = (slug: string) => `spiffe://runkora.io/tenant/${TENANT}/agent/${slug}`;

interface StoryPrincipal {
  subject: string;
  type: EventPrincipalType;
  name: string;
}

const PRINCIPALS: StoryPrincipal[] = [
  { subject: 'usr_42b7e9', type: 'USER', name: 'Dev Operator' },
  { subject: 'usr_8c1f20', type: 'USER', name: 'Priya Raman' },
  { subject: 'usr_3d9a51', type: 'USER', name: 'Sam Okafor' },
  { subject: agentSubject('triage'), type: 'AGENT', name: 'triage' },
  { subject: agentSubject('pr-reviewer'), type: 'AGENT', name: 'pr-reviewer' },
  { subject: agentSubject('cost-sweeper'), type: 'AGENT', name: 'cost-sweeper' },
  { subject: agentSubject('release-notes'), type: 'AGENT', name: 'release-notes' },
  { subject: agentSubject('incident-notifier'), type: 'AGENT', name: 'incident-notifier' },
  { subject: 'trigger:trg_01JC7Q2M', type: 'TRIGGER', name: 'sentry-new-issue-triage' },
  { subject: 'trigger:trg_01JD0A1X', type: 'TRIGGER', name: 'nightly-dependency-audit' },
  { subject: 'trigger:trg_01JE4N8R', type: 'TRIGGER', name: 'on-triage-failed-notify' },
  { subject: 'kora-agents', type: 'SYSTEM', name: 'kora-agents' },
  { subject: 'kora-triggers', type: 'SYSTEM', name: 'kora-triggers' },
  {
    subject: 'inference-gateway',
    type: 'SYSTEM',
    name: 'inference-gateway',
  },
  { subject: 'operator', type: 'SYSTEM', name: 'operator' },
];

const who = (name: string): StoryPrincipal => {
  const p = PRINCIPALS.find((x) => x.name === name);
  if (p === undefined) throw new Error(`unknown principal ${name}`);
  return p;
};

const RUN = {
  failedTriage: '2b4e8c1d-5f7a-4c2e-9b1d-0e6f3a8c7d21',
  notifier: '7c1d9e40-2a6b-4f13-8e5c-91b0d2f4a6e3',
  review: '5e0a3b7f-c812-4d69-a1f4-3b7e9c20d815',
  sweep: '91f2c6a8-04d3-4b7e-b5a9-6c8e1f3d2a47',
  notes: 'c43a8e15-7b20-49d6-8f31-2e5d0c9b7a16',
  replaced: 'e8b17d42-3c95-4a60-9d2e-7f1a4b8c3e59',
  idleOut: '0d6f2a91-8e47-4c3b-a1d5-5b9e7c0f2d38',
} as const;

const AGENT_ID: Record<string, string> = {
  triage: '3f6a2c10-8b4e-4d21-9a7c-1e5f0b3d9c42',
  'pr-reviewer': '8d2e5a71-0c3f-4b96-a1e8-5f7c2d4b6a03',
  'cost-sweeper': 'b41c9e27-6d5a-4f08-8e3b-2a9d7c1f5e64',
  'release-notes': '6e7f3b18-2a4c-4d59-b0e1-9c8a5d2f7b31',
  'incident-notifier': 'c9a4d2e6-1f7b-4038-9d5e-7b2c6a1e4f85',
};

/** A run as the agents.run events carry it. */
const runEntity = (id: string, agent: string, state: string, extra: object = {}) => ({
  id,
  agent_id: AGENT_ID[agent] ?? agent,
  agent,
  state,
  created_by: 'usr_42b7e9',
  ...extra,
});

/** A run's sandbox as the agents.sandbox events carry it. */
const sandboxEntity = (runId: string, environment: string, extra: object = {}) => ({
  run_id: runId,
  name: `sandbox-${runId}`,
  environment,
  ...extra,
});

/** A trigger as the triggers.trigger events carry it. */
const triggerEntity = (
  id: string,
  name: string,
  kind: string,
  agent: string,
  extra: object = {},
) => ({
  id,
  name,
  kind,
  target_kind: 'agent',
  agent_type_id: AGENT_ID[agent] ?? agent,
  agent_slug: agent,
  enabled: true,
  max_parallel: 1,
  overflow: 'queue',
  max_queue_depth: 10,
  timeout_seconds: 600,
  dedupe_enabled: true,
  autopause_enabled: true,
  source: 'api',
  ...extra,
});

/** A UUIDv7-shaped id: the time in the first 48 bits, then a counter, so ids sort by time. */
function uuid7(ms: number, n: number): string {
  const t = Math.floor(ms).toString(16).padStart(12, '0');
  const c = n.toString(16).padStart(15, '0');
  return `${t.slice(0, 8)}-${t.slice(8, 12)}-7${c.slice(0, 3)}-9${c.slice(3, 6)}-${c.slice(6, 15)}abc`;
}

function traceparent(n: number): string {
  const trace = (0x4bf92f35 + n * 7919).toString(16).padStart(8, '0');
  return `00-${trace}77b34da6a3ce929d0e0e47${n.toString(16).padStart(2, '0')}-00f067aa0ba902b7-01`;
}

interface Spec {
  /** Minutes before `now`. */
  ago: number;
  type: string;
  by: string;
  subject: string;
  entity: Record<string, unknown>;
  changed?: string[];
  version?: number;
  run?: string;
  agent?: string;
  delegator?: string;
  /** Index into the spec list of the event that caused this one. */
  causedBy?: number;
  depth?: number;
  triggers?: string[];
  source?: string;
  /** Events with no trace leave it out. */
  noTrace?: boolean;
}

const SERVICE_OF: Record<string, string> = {
  agents: 'kora-agents',
  triggers: 'kora-triggers',
  inference: 'inference-gateway',
  identity: 'identity',
  policy: 'policy-service',
  declarative: 'operator',
};

const FAILED_RUN_EVENT = 8;

const SPECS: Spec[] = [
  {
    ago: 1,
    type: 'agents.run.completed.v1',
    by: 'incident-notifier',
    subject: `runs/${RUN.notifier}`,
    entity: runEntity(RUN.notifier, 'incident-notifier', 'completed', {
      trigger_id: 'trg_01JE4N8R',
    }),
    changed: ['state'],
    version: 5,
    run: RUN.notifier,
    agent: 'incident-notifier',
    causedBy: FAILED_RUN_EVENT,
    depth: 1,
    triggers: ['trg_01JE4N8R'],
  },
  {
    ago: 2,
    type: 'inference.budget.threshold_crossed.v1',
    by: 'inference-gateway',
    subject: 'budgets/principal/cost-sweeper',
    entity: {
      scope: 'principal',
      id: 'cost-sweeper',
      window: 'monthly',
      period_start: '2026-11-01T00:00:00Z',
      limit_usd: 200,
      spend_usd: 161.4,
      level: 80,
    },
    agent: 'cost-sweeper',
  },
  {
    ago: 3,
    type: 'policy.grant.updated.v1',
    by: 'Dev Operator',
    subject: 'grants/grt_7f3a91c4',
    entity: {
      id: 'grt_7f3a91c4',
      principal_type: 'AGENT',
      principal_id: 'triage',
      scopes: ['mcp:github:issues:write', 'mcp:github:pulls:read', 'mcp:sentry:issues:read'],
      budget: { limit_usd: 50, reset_cadence: 'monthly' },
      delegation_required: false,
      created_at: '2026-09-02T10:00:00Z',
    },
    changed: ['scopes', 'budget'],
    version: 4,
    noTrace: false,
  },
  {
    ago: 4,
    type: 'agents.run.started.v1',
    by: 'incident-notifier',
    subject: `runs/${RUN.notifier}`,
    entity: runEntity(RUN.notifier, 'incident-notifier', 'active', { trigger_id: 'trg_01JE4N8R' }),
    changed: ['state'],
    version: 3,
    run: RUN.notifier,
    agent: 'incident-notifier',
    causedBy: FAILED_RUN_EVENT,
    depth: 1,
    triggers: ['trg_01JE4N8R'],
  },
  {
    ago: 4.2,
    type: 'agents.sandbox.ready.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.notifier}/sandbox`,
    entity: sandboxEntity(RUN.notifier, 'python-3-12', {
      image: 'ghcr.io/example-org/agent-base:2.3',
      egress: 'passed',
    }),
    run: RUN.notifier,
    agent: 'incident-notifier',
    causedBy: FAILED_RUN_EVENT,
    depth: 1,
    triggers: ['trg_01JE4N8R'],
  },
  {
    ago: 4.8,
    type: 'agents.sandbox.created.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.notifier}/sandbox`,
    entity: sandboxEntity(RUN.notifier, 'python-3-12'),
    run: RUN.notifier,
    agent: 'incident-notifier',
    causedBy: FAILED_RUN_EVENT,
    depth: 1,
    triggers: ['trg_01JE4N8R'],
  },
  {
    ago: 5,
    type: 'agents.run.created.v1',
    by: 'on-triage-failed-notify',
    subject: `runs/${RUN.notifier}`,
    entity: runEntity(RUN.notifier, 'incident-notifier', 'pending', { trigger_id: 'trg_01JE4N8R' }),
    version: 1,
    run: RUN.notifier,
    agent: 'incident-notifier',
    causedBy: FAILED_RUN_EVENT,
    depth: 1,
    triggers: ['trg_01JE4N8R'],
  },
  {
    ago: 6,
    type: 'triggers.delivery.failed.v1',
    by: 'sentry-new-issue-triage',
    subject: 'deliveries/dlv_920ff1e7',
    entity: {
      id: 'dlr_5d20ab31',
      delivery_id: 'dlv_920ff1e7',
      trigger_id: 'trg_01JC7Q2M',
      trigger_name: 'sentry-new-issue-triage',
      status: 'failed',
      reason: 'dispatch_failed',
    },
    agent: 'triage',
  },
  {
    ago: 7,
    type: 'agents.run.failed.v1',
    by: 'triage',
    subject: `runs/${RUN.failedTriage}`,
    entity: runEntity(RUN.failedTriage, 'triage', 'failed', {
      trigger_id: 'trg_01JC7Q2M',
      delivery_id: 'dlv_920ff1e7',
      error: 'sandbox_failed',
    }),
    changed: ['state', 'error'],
    version: 7,
    run: RUN.failedTriage,
    agent: 'triage',
    triggers: ['trg_01JC7Q2M'],
  },
  {
    ago: 7.3,
    type: 'agents.sandbox.failed.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.failedTriage}/sandbox`,
    entity: sandboxEntity(RUN.failedTriage, 'triage-node-22', {
      image: 'ghcr.io/example-org/triage-sandbox:1.4',
    }),
    run: RUN.failedTriage,
    agent: 'triage',
  },
  {
    ago: 8,
    type: 'agents.sandbox.created.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.failedTriage}/sandbox`,
    entity: sandboxEntity(RUN.failedTriage, 'triage-node-22'),
    run: RUN.failedTriage,
    agent: 'triage',
  },
  {
    ago: 8.2,
    type: 'agents.run.created.v1',
    by: 'sentry-new-issue-triage',
    subject: `runs/${RUN.failedTriage}`,
    entity: runEntity(RUN.failedTriage, 'triage', 'pending', {
      trigger_id: 'trg_01JC7Q2M',
      delivery_id: 'dlv_920ff1e7',
    }),
    version: 1,
    run: RUN.failedTriage,
    agent: 'triage',
    triggers: ['trg_01JC7Q2M'],
  },
  {
    ago: 11,
    type: 'agents.run.guardrail_warned.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.review}`,
    entity: runEntity(RUN.review, 'pr-reviewer', 'active', {
      guardrail: { kind: 'repeated_call', count: 5, tool: 'github.get_file' },
    }),
    run: RUN.review,
    agent: 'pr-reviewer',
    delegator: 'usr_8c1f20',
  },
  {
    ago: 12,
    type: 'agents.run.message_received.v1',
    by: 'Priya Raman',
    subject: `runs/${RUN.review}`,
    entity: runEntity(RUN.review, 'pr-reviewer', 'active', { sender: 'person' }),
    run: RUN.review,
    agent: 'pr-reviewer',
  },
  {
    ago: 14,
    type: 'agents.run.idle.v1',
    by: 'pr-reviewer',
    subject: `runs/${RUN.review}`,
    entity: runEntity(RUN.review, 'pr-reviewer', 'idle'),
    changed: ['state'],
    version: 4,
    run: RUN.review,
    agent: 'pr-reviewer',
    delegator: 'usr_8c1f20',
  },
  {
    ago: 16,
    type: 'agents.run.started.v1',
    by: 'pr-reviewer',
    subject: `runs/${RUN.review}`,
    entity: runEntity(RUN.review, 'pr-reviewer', 'active'),
    changed: ['state'],
    version: 2,
    run: RUN.review,
    agent: 'pr-reviewer',
    delegator: 'usr_8c1f20',
  },
  {
    ago: 16.4,
    type: 'identity.delegation.issued.v1',
    by: 'Priya Raman',
    subject: `delegations/${RUN.review}`,
    entity: {
      run_id: RUN.review,
      agent_principal_id: agentSubject('pr-reviewer'),
      delegator_principal_id: 'usr_8c1f20',
      delegator_principal_type: 'HUMAN',
      created_at: '2026-11-06T07:00:00Z',
    },
    agent: 'pr-reviewer',
  },
  {
    ago: 17,
    type: 'agents.run.created.v1',
    by: 'Priya Raman',
    subject: `runs/${RUN.review}`,
    entity: runEntity(RUN.review, 'pr-reviewer', 'pending'),
    version: 1,
    run: RUN.review,
    agent: 'pr-reviewer',
  },
  {
    ago: 18,
    type: 'identity.session.created.v1',
    by: 'Priya Raman',
    subject: 'sessions/ses_9b04c2',
    entity: {
      id: 'ses_9b04c2',
      principal_id: 'usr_8c1f20',
      principal_type: 'HUMAN',
      display_name: 'Priya Raman',
      identity_source: 'FEDERATED',
      created_at: '2026-11-06T06:58:00Z',
      expires_at: '2026-11-06T14:58:00Z',
    },
  },
  {
    ago: 22,
    type: 'triggers.trigger.autopaused.v1',
    by: 'kora-triggers',
    subject: 'triggers/trg_01JD0A1X',
    entity: triggerEntity(
      'trg_01JD0A1X',
      'nightly-dependency-audit',
      'scheduled',
      'dependency-auditor',
      { enabled: false, auto_paused_at: '2026-11-06T06:40:00Z' },
    ),
    changed: ['enabled'],
    version: 12,
    agent: 'dependency-auditor',
  },
  {
    ago: 23,
    type: 'triggers.delivery.denied.v1',
    by: 'nightly-dependency-audit',
    subject: 'deliveries/dlv_4c81aa02',
    entity: {
      id: 'dlr_8a07c4e2',
      delivery_id: 'dlv_4c81aa02',
      trigger_id: 'trg_01JD0A1X',
      trigger_name: 'nightly-dependency-audit',
      status: 'denied',
      reason: 'forbidden',
    },
    agent: 'dependency-auditor',
  },
  {
    ago: 35,
    type: 'agents.run.budget_exhausted.v1',
    by: 'inference-gateway',
    subject: `runs/${RUN.sweep}`,
    entity: runEntity(RUN.sweep, 'cost-sweeper', 'budget_exhausted'),
    changed: ['state'],
    version: 9,
    run: RUN.sweep,
    agent: 'cost-sweeper',
  },
  {
    ago: 36,
    type: 'inference.budget.exceeded.v1',
    by: 'inference-gateway',
    subject: 'budgets/principal/cost-sweeper',
    entity: {
      scope: 'principal',
      id: 'cost-sweeper',
      window: 'daily',
      period_start: '2026-11-06T00:00:00Z',
      limit_usd: 20,
      spend_usd: 20.07,
    },
    run: RUN.sweep,
    agent: 'cost-sweeper',
  },
  {
    ago: 38,
    type: 'inference.budget.threshold_crossed.v1',
    by: 'inference-gateway',
    subject: 'budgets/principal/cost-sweeper',
    entity: {
      scope: 'principal',
      id: 'cost-sweeper',
      window: 'daily',
      period_start: '2026-11-06T00:00:00Z',
      limit_usd: 20,
      spend_usd: 20.01,
      level: 100,
    },
    run: RUN.sweep,
    agent: 'cost-sweeper',
  },
  {
    ago: 52,
    type: 'agents.run.terminated.v1',
    by: 'Sam Okafor',
    subject: `runs/${RUN.notes}`,
    entity: runEntity(RUN.notes, 'release-notes', 'terminated', {
      deleted_at: '2026-11-06T06:08:00Z',
    }),
    changed: ['state'],
    version: 8,
    run: RUN.notes,
    agent: 'release-notes',
  },
  {
    ago: 55,
    type: 'agents.run.interrupted.v1',
    by: 'Sam Okafor',
    subject: `runs/${RUN.notes}`,
    entity: runEntity(RUN.notes, 'release-notes', 'idle'),
    run: RUN.notes,
    agent: 'release-notes',
  },
  {
    ago: 58,
    type: 'agents.run.errored.v1',
    by: 'release-notes',
    subject: `runs/${RUN.notes}`,
    entity: runEntity(RUN.notes, 'release-notes', 'idle', {
      error: 'tool call timed out: github.list_pull_requests',
    }),
    run: RUN.notes,
    agent: 'release-notes',
    delegator: 'usr_3d9a51',
  },
  {
    ago: 70,
    type: 'agents.environment.updated.v1',
    by: 'Dev Operator',
    subject: 'environments/python-3-12',
    entity: {
      id: 'env_python-3-12',
      slug: 'python-3-12',
      name: 'python-3-12',
      sandbox_image: 'ghcr.io/example-org/agent-base:2.3',
      size: { cpu: '2', memory: '4Gi', disk: '20Gi' },
      connections: ['github'],
    },
    changed: ['sandbox_image'],
    version: 6,
  },
  {
    ago: 72,
    type: 'agents.sandbox.replaced.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.replaced}/sandbox`,
    entity: sandboxEntity(RUN.replaced, 'python-3-12', {
      image: 'ghcr.io/example-org/agent-base:2.3',
    }),
    run: RUN.replaced,
    agent: 'triage',
  },
  {
    ago: 80,
    type: 'identity.pat.created.v1',
    by: 'Dev Operator',
    subject: 'pats/pat_61c0f9',
    entity: {
      id: 'pat_61c0f9',
      user_id: 'usr_42b7e9',
      name: 'ci-deploy',
      scopes: ['agents:read', 'triggers:fire'],
      expires_at: '2027-05-06T00:00:00Z',
      created_at: '2026-11-06T05:30:00Z',
    },
  },
  {
    ago: 85,
    type: 'identity.pat.updated.v1',
    by: 'Sam Okafor',
    subject: 'pats/pat_2f7d13',
    entity: {
      id: 'pat_2f7d13',
      user_id: 'usr_3d9a51',
      name: 'local-cli',
      scopes: ['agents:read', 'agents:write'],
      expires_at: '2027-02-01T00:00:00Z',
      created_at: '2026-08-01T00:00:00Z',
    },
    changed: ['scopes'],
    version: 3,
  },
  {
    ago: 100,
    type: 'identity.pat.revoked.v1',
    by: 'Dev Operator',
    subject: 'pats/pat_0a9e44',
    entity: {
      id: 'pat_0a9e44',
      user_id: 'usr_8c1f20',
      name: 'old-laptop',
      scopes: ['agents:read'],
      expires_at: '2027-01-01T00:00:00Z',
      created_at: '2026-06-01T00:00:00Z',
      revoked_at: '2026-11-06T04:50:00Z',
    },
    changed: ['revoked_at'],
    version: 2,
  },
  {
    ago: 120,
    type: 'declarative.resource.failed.v1',
    by: 'operator',
    subject: 'declared/triggers/review-opened-prs',
    entity: {
      kind: 'triggers',
      namespace: TENANT,
      name: 'review-opened-prs',
      uid: '6c1e0f52-7a3b-4d8e-9f21-0b5c7d3e8a14',
      generation: 3,
      outcome: 'NameConflict',
    },
    noTrace: true,
  },
  {
    ago: 130,
    type: 'triggers.trigger.created.v1',
    by: 'operator',
    subject: 'triggers/trg_01JF2B6W',
    entity: triggerEntity('trg_01JF2B6W', 'pr-merged-release-notes', 'webhook', 'release-notes', {
      source: 'crd',
      repository: 'github.com/example-org/kora-config',
    }),
    version: 1,
    agent: 'release-notes',
  },
  {
    ago: 150,
    type: 'policy.role.created.v1',
    by: 'Dev Operator',
    subject: 'roles/release-managers',
    entity: {
      id: 'rol_release-managers',
      role: 'release-managers',
      scopes: ['agents:run:release-notes', 'triggers:fire'],
      created_at: '2026-11-06T04:00:00Z',
      source: 'api',
    },
    version: 1,
  },
  {
    ago: 165,
    type: 'agents.agent.updated.v1',
    by: 'Sam Okafor',
    subject: 'agents/release-notes',
    entity: {
      id: 'agt_release-notes',
      slug: 'release-notes',
      name: 'release-notes',
      model_id: 'release-writer',
      state: 'active',
      identity_mode: 'autonomous',
      environment_ids: ['env_python-3-12'],
      connections: ['github'],
    },
    changed: ['model_id', 'description'],
    version: 11,
    agent: 'release-notes',
  },
  {
    ago: 180,
    type: 'identity.tenant.switched.v1',
    by: 'Sam Okafor',
    subject: 'sessions/ses_4e1b77',
    entity: { session_id: 'ses_4e1b77', principal_id: 'usr_42b7e9', direction: 'in' },
  },
  {
    ago: 200,
    type: 'policy.tenant_budget.updated.v1',
    by: 'Dev Operator',
    subject: 'tenant_budgets/kora-tenant-example',
    entity: { limit_usd: 5000, reset_cadence: 'monthly', source: 'api' },
    changed: ['limit_usd'],
    version: 3,
  },
  {
    ago: 240,
    type: 'triggers.source.updated.v1',
    by: 'Dev Operator',
    subject: 'sources/src_example-org-github',
    entity: {
      id: 'src_example-org-github',
      name: 'example-org-github',
      provider: 'github',
      enabled: true,
      source: 'api',
    },
    changed: ['config'],
    version: 5,
  },
  {
    ago: 300,
    type: 'agents.sandbox.deleted.v1',
    by: 'kora-agents',
    subject: `runs/${RUN.idleOut}/sandbox`,
    entity: sandboxEntity(RUN.idleOut, 'triage-node-22'),
    run: RUN.idleOut,
    agent: 'triage',
  },
  {
    ago: 301,
    type: 'agents.run.completed.v1',
    by: 'triage',
    subject: `runs/${RUN.idleOut}`,
    entity: runEntity(RUN.idleOut, 'triage', 'completed'),
    changed: ['state'],
    version: 6,
    run: RUN.idleOut,
    agent: 'triage',
    triggers: ['trg_01JC7Q2M'],
  },
  {
    ago: 360,
    type: 'policy.grant.deleted.v1',
    by: 'Priya Raman',
    subject: 'grants/grt_19b2e0d7',
    entity: {
      id: 'grt_19b2e0d7',
      principal_type: 'PAT',
      principal_id: 'ci-legacy',
      scopes: ['agents:read'],
      delegation_required: false,
      created_at: '2026-03-10T00:00:00Z',
    },
    version: 7,
  },
];

function envelope(spec: Spec, n: number, at: number, ids: string[]): KoraEvent {
  const principal = who(spec.by);
  const time = new Date(at).toISOString();
  const domain = spec.type.split('.')[0] ?? '';
  return {
    specversion: '1.0',
    id: uuid7(at, n),
    source: spec.source ?? SERVICE_OF[domain] ?? 'kora-agents',
    type: spec.type,
    subject: spec.subject,
    time,
    datacontenttype: 'application/json',
    koratenant: TENANT,
    koraprincipal: principal.subject,
    koraprincipaltype: principal.type,
    koraprincipalname: principal.name,
    ...(spec.delegator !== undefined && { koradelegator: spec.delegator }),
    ...(spec.run !== undefined && { korarun: spec.run }),
    ...(spec.agent !== undefined && { koraagent: spec.agent }),
    ...(spec.causedBy !== undefined && { koracausedby: ids[spec.causedBy] }),
    korachaindepth: spec.depth ?? 0,
    ...(spec.triggers !== undefined && { koratriggers: spec.triggers }),
    ...(spec.noTrace !== true && { traceparent: traceparent(n) }),
    data: {
      entity: spec.entity,
      ...(spec.changed !== undefined && { changed: spec.changed }),
      entity_version: spec.version ?? 1,
      occurred_at: new Date(at - 14).toISOString(),
    },
  };
}

/** The tenant's history, newest first, timed back from `now`. */
export function buildEvents(now: number = Date.now()): KoraEvent[] {
  const ids = SPECS.map((s, n) => uuid7(now - s.ago * 60_000, n));
  return SPECS.map((s, n) => envelope(s, n, now - s.ago * 60_000, ids));
}

/** The chart's trace URL template, as the server passes it to the page. */
export const TRACE_URL_TEMPLATE = 'https://grafana.example.com/explore?traceId={trace_id}';
