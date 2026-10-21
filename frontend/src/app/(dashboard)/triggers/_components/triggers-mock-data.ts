import type {
  ModelObject,
  SchedulePreviewResponse,
  TriggerDelivery,
  TriggerEventTypes,
  TriggerField,
  TriggerRoutingCandidate,
  TriggerRoutingDecision,
} from '@runkora/shared/api';
import { toDeliveryRow, toTriggerRow } from '../_types';
import type { AgentOption, DeliveryRow, DeliveryRunRow, SourceRow, TriggerRow } from '../_types';

/** Mock data for the Triggers page stories. Timestamps are relative to load time so "5m ago" reads right. */

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

/** An ISO timestamp this far in the past. */
function ago(ms: number): string {
  return new Date(Date.now() - ms).toISOString();
}

// ── Agents and sources ───────────────────────────────────────────────────────

export const AGENTS: AgentOption[] = [
  {
    id: 'at_8f3a1c92',
    slug: 'code-reviewer',
    name: 'Code Reviewer',
    identityMode: 'autonomous',
    canRun: true,
  },
  { id: 'at_21b7d4e0', slug: 'coder', name: 'Coder', identityMode: 'autonomous', canRun: true },
  {
    id: 'at_5c90aa17',
    slug: 'security-reviewer',
    name: 'Security Reviewer',
    identityMode: 'autonomous',
    canRun: true,
  },
  {
    id: 'at_77e2f0b3',
    slug: 'docs-writer',
    name: 'Docs Writer',
    identityMode: 'autonomous',
    canRun: true,
  },
  {
    id: 'at_09d6c5a8',
    slug: 'triage-bot',
    name: 'Triage Bot',
    identityMode: 'autonomous',
    canRun: true,
  },
  {
    id: 'at_c41e8b6d',
    slug: 'personal-assistant',
    name: 'Personal Assistant',
    identityMode: 'delegated',
    canRun: true,
  },
];

const agent = (slug: string): AgentOption => {
  const found = AGENTS.find((a) => a.slug === slug);
  if (found === undefined) throw new Error(`unknown mock agent ${slug}`);
  return found;
};

export const SOURCES: SourceRow[] = [
  {
    id: 'src_4d1f9a7c',
    name: 'acme-github',
    provider: 'github',
    category: 'webhook',
    enabled: true,
    signingSecretSet: true,
    createdAt: ago(41 * DAY),
    rejectionCount: 0,
    lastRejectionAt: null,
    lastRejectionReason: null,
  },
  {
    id: 'src_b82e3016',
    name: 'sentry-prod',
    provider: 'sentry',
    category: 'webhook',
    enabled: true,
    signingSecretSet: true,
    createdAt: ago(27 * DAY),
    rejectionCount: 3,
    lastRejectionAt: ago(2 * HOUR),
    lastRejectionReason: 'signature mismatch',
  },
  {
    id: 'src_0a6c57de',
    name: 'linear-workspace',
    provider: 'linear',
    category: 'webhook',
    enabled: true,
    signingSecretSet: true,
    createdAt: ago(19 * DAY),
    rejectionCount: 0,
    lastRejectionAt: null,
    lastRejectionReason: null,
  },
  {
    id: 'src_7c3e51b9',
    name: 'acme-slack',
    provider: 'slack',
    category: 'chat',
    enabled: true,
    signingSecretSet: true,
    createdAt: ago(6 * DAY),
    rejectionCount: 0,
    lastRejectionAt: null,
    lastRejectionReason: null,
  },
];

// ── Triggers ─────────────────────────────────────────────────────────────────

/** A trigger with the platform defaults, so each entry spells out only what is distinctive. */
function trigger(
  overrides: Partial<TriggerRow> & Pick<TriggerRow, 'id' | 'name' | 'kind' | 'agentSlug'>,
): TriggerRow {
  const owner = agent(overrides.agentSlug);
  return {
    sourceId: '',
    targetKind: 'agent',
    agentTypeId: owner.id,
    routing: null,
    eventType: '',
    matchCondition: {},
    matchContains: {},
    senderRestriction: { ids: [], roles: [] },
    coalesce: null,
    promptTemplate: '',
    enabled: true,
    createdAt: ago(20 * DAY),
    maxParallel: 4,
    overflow: 'wait',
    maxQueueDepth: 32,
    timeoutSeconds: 30,
    dedupeEnabled: true,
    autopauseEnabled: true,
    consecutiveFailures: 0,
    autoPausedAt: null,
    pendingCount: 0,
    canFire: true,
    ...overrides,
  };
}

/** The trigger the drawer stories open: a webhook with several filters, one on a long field path. */
export const PR_REVIEW_TRIGGER = trigger({
  id: 'trg_1c8e04b2',
  name: 'review-opened-prs',
  kind: 'webhook',
  agentSlug: 'code-reviewer',
  sourceId: 'src_4d1f9a7c',
  eventType: 'pull_request',
  matchCondition: {
    'payload.action': ['opened', 'reopened', 'ready_for_review'],
    'payload.pull_request.base.repo.default_branch': ['main'],
    'payload.pull_request.draft': ['false'],
  },
  matchContains: {
    'payload.pull_request.head.repo.full_name': ['acme/'],
  },
  senderRestriction: { ids: ['dependabot[bot]', 'jwilkin'], roles: ['MEMBER', 'OWNER'] },
  coalesce: {
    key: '{{payload.repository.full_name}}#{{payload.pull_request.number}}',
    windowSeconds: 30,
  },
  promptTemplate:
    'Review pull request #{{payload.pull_request.number}} in {{payload.repository.full_name}}.\n\nTitle: {{payload.pull_request.title}}\nAuthor: {{payload.sender.login}}\n\nLook for correctness bugs, missing tests and risky migrations. Leave inline comments and finish with a short summary.',
  maxParallel: 6,
  overflow: 'wait',
  maxQueueDepth: 64,
  timeoutSeconds: 120,
  createdAt: ago(38 * DAY),
  pendingCount: 2,
});

/**
 * A chat command trigger: stored as a webhook trigger on the Slack source,
 * limited to two channels and two people.
 */
export const SLACK_MENTION_TRIGGER = trigger({
  id: 'trg_4b9e1d27',
  name: 'answer-platform-questions',
  kind: 'webhook',
  agentSlug: 'triage-bot',
  sourceId: 'src_7c3e51b9',
  eventType: 'mention',
  matchCondition: { 'conversation.id': ['C04PLATFORM', 'C07ONCALL'] },
  senderRestriction: { ids: ['U012AB3CD', 'U045EF6GH'], roles: [] },
  coalesce: { key: '{{ conversation.id }}:{{ thread.id }}', windowSeconds: 10 },
  promptTemplate:
    'Answer {{ text }} from {{ user.id }}. Reply in thread {{ thread.id }} of conversation {{ conversation.id }}.',
  maxParallel: 4,
  createdAt: ago(5 * DAY),
});

export const TRIGGERS: TriggerRow[] = [
  PR_REVIEW_TRIGGER,
  SLACK_MENTION_TRIGGER,
  trigger({
    id: 'trg_d2a6f08e',
    name: 'personal-dm-helper',
    kind: 'webhook',
    agentSlug: 'docs-writer',
    sourceId: 'src_7c3e51b9',
    eventType: 'direct_message',
    promptTemplate:
      'Help {{ user.id }} with {{ text }}. Reply in conversation {{ conversation.id }}.',
    createdAt: ago(4 * DAY),
  }),
  trigger({
    id: 'trg_7a29d5f1',
    name: 'ci-failed-fixer',
    kind: 'webhook',
    agentSlug: 'coder',
    sourceId: 'src_4d1f9a7c',
    eventType: 'check_run',
    matchCondition: { 'payload.check_run.conclusion': ['failure'] },
    promptTemplate:
      'The check {{payload.check_run.name}} failed on {{payload.repository.full_name}}. Find the cause and open a fix.',
    createdAt: ago(33 * DAY),
    overflow: 'replace',
  }),
  trigger({
    id: 'trg_e30b8c46',
    name: 'sentry-new-issue-triage',
    kind: 'webhook',
    agentSlug: 'triage-bot',
    sourceId: 'src_b82e3016',
    eventType: 'issue.created',
    matchCondition: { 'payload.data.issue.level': ['error', 'fatal'] },
    promptTemplate:
      'A new Sentry issue was raised: {{payload.data.issue.title}}. Work out the owning service and open a ticket.',
    consecutiveFailures: 2,
    createdAt: ago(25 * DAY),
  }),
  trigger({
    id: 'trg_92f6a1d8',
    name: 'incident-summariser',
    kind: 'webhook',
    agentSlug: 'docs-writer',
    sourceId: 'src_b82e3016',
    eventType: 'alert.triggered',
    promptTemplate: 'Summarise the alert {{payload.data.alert.name}} for the incident channel.',
    consecutiveFailures: 5,
    createdAt: ago(24 * DAY),
  }),
  trigger({
    id: 'trg_3b5d0e79',
    name: 'linear-assign-to-coder',
    kind: 'webhook',
    agentSlug: 'coder',
    sourceId: 'src_0a6c57de',
    eventType: 'Issue.update',
    matchCondition: { 'payload.data.assignee.name': ['Coder'] },
    promptTemplate: 'Pick up {{payload.data.identifier}}: {{payload.data.title}}.',
    createdAt: ago(18 * DAY),
  }),
  trigger({
    id: 'trg_c1a74e20',
    name: 'secret-scanning-alert',
    kind: 'webhook',
    agentSlug: 'security-reviewer',
    sourceId: 'src_4d1f9a7c',
    eventType: 'secret_scanning_alert',
    matchCondition: { 'payload.action': ['created'] },
    promptTemplate:
      'A secret was detected in {{payload.repository.full_name}}. Assess exposure and draft the rotation steps.',
    enabled: false,
    createdAt: ago(30 * DAY),
  }),
  trigger({
    id: 'trg_5e8d2b03',
    name: 'weekly-security-sweep',
    kind: 'scheduled',
    agentSlug: 'security-reviewer',
    scheduleConfig: { cronExpression: '0 6 * * 1', timezone: 'Australia/Sydney', catchUp: false },
    promptTemplate:
      'Audit dependencies across the acme org for new advisories and summarise anything actionable.',
    maxParallel: 1,
    overflow: 'replace',
    createdAt: ago(35 * DAY),
  }),
  trigger({
    id: 'trg_a4f03c91',
    name: 'nightly-dependency-audit',
    kind: 'scheduled',
    agentSlug: 'coder',
    scheduleConfig: { cronExpression: '0 2 * * *', timezone: 'UTC', catchUp: true },
    promptTemplate: 'Check for outdated dependencies and open one pull request per safe upgrade.',
    maxParallel: 1,
    overflow: 'replace',
    createdAt: ago(29 * DAY),
  }),
  trigger({
    id: 'trg_6d19b7e4',
    name: 'queue-health-check',
    kind: 'scheduled',
    agentSlug: 'triage-bot',
    scheduleConfig: { cronExpression: '*/15 * * * *', timezone: 'UTC' },
    promptTemplate: 'Inspect the deliveries queue and report stuck or retrying items.',
    maxParallel: 1,
    overflow: 'replace',
    consecutiveFailures: 5,
    autoPausedAt: ago(3 * HOUR),
    enabled: false,
    createdAt: ago(22 * DAY),
  }),
  trigger({
    id: 'trg_f82c0a35',
    name: 'draft-release-notes',
    kind: 'manual',
    agentSlug: 'docs-writer',
    manualInputs: [
      { name: 'version', type: 'text', label: 'Version', required: true, default: 'v1.8.0' },
      {
        name: 'audience',
        type: 'choice',
        label: 'Audience',
        required: true,
        choices: ['customers', 'internal', 'changelog'],
      },
      { name: 'include_breaking', type: 'boolean', label: 'Highlight breaking changes' },
    ],
    promptTemplate:
      'Draft release notes for {{version}} aimed at {{audience}}. Highlight breaking changes: {{include_breaking}}.',
    createdAt: ago(14 * DAY),
  }),
  trigger({
    id: 'trg_0b73e9c6',
    name: 'rerun-flaky-tests',
    kind: 'manual',
    agentSlug: 'coder',
    manualInputs: [
      { name: 'pattern', type: 'text', label: 'Test pattern', required: true },
      { name: 'attempts', type: 'number', label: 'Attempts', default: '5' },
    ],
    promptTemplate: 'Run tests matching {{pattern}} {{attempts}} times and report which are flaky.',
    createdAt: ago(9 * DAY),
  }),
];

// ── Deliveries ───────────────────────────────────────────────────────────────

let deliverySeq = 0;

/** A delivery with sensible defaults; the id and run fan-out derive from the trigger it names. */
function delivery(
  t: TriggerRow,
  ageMs: number,
  status: string,
  extra: Partial<DeliveryRow> = {},
): DeliveryRow {
  deliverySeq += 1;
  const receivedAt = ago(ageMs);
  const dispatched = status === 'dispatched';
  const runId = dispatched ? `run_${(0x3f5a00 + deliverySeq * 977).toString(16)}b2` : null;
  const outcome: DeliveryRunRow = {
    triggerId: t.id,
    triggerName: t.name,
    runId,
    status,
    error: extra.error ?? null,
    createdAt: new Date(Date.parse(receivedAt) + 180 + (deliverySeq % 7) * 140).toISOString(),
  };
  return {
    id: `dlv_${(0x91c000 + deliverySeq * 4093).toString(16)}e7`,
    kind: t.kind,
    sourceId: t.sourceId,
    eventType: t.eventType,
    status,
    triggerId: t.id,
    triggerName: t.name,
    runId,
    error: null,
    payloadExcerpt: null,
    firedBy: null,
    scheduledAt: null,
    reason: null,
    payloadBytes: null,
    payloadPurged: false,
    receivedAt,
    runs: [outcome],
    coalesceKeys: [],
    ...extra,
  };
}

const byName = (name: string): TriggerRow => {
  const found = TRIGGERS.find((t) => t.name === name);
  if (found === undefined) throw new Error(`unknown mock trigger ${name}`);
  return found;
};

const PR_EXCERPT =
  '{"action":"opened","number":1427,"pull_request":{"title":"Add retry budget to the outbound dispatcher","user":{"login":"mchen"},"base":{"ref":"main"}},"repository":{"full_name":"acme/platform"}}';
const CI_EXCERPT =
  '{"action":"completed","check_run":{"name":"unit-tests","conclusion":"failure"},"repository":{"full_name":"acme/api"}}';
const SENTRY_EXCERPT =
  '{"action":"created","data":{"issue":{"title":"TypeError: Cannot read properties of undefined (reading \'id\')","level":"error"}}}';

const SLACK_EXCERPT =
  '{"type":"event_callback","team_id":"T01ACME","event_id":"Ev08QX4","event":{"type":"app_mention","user":"U012AB3CD","channel":"C04PLATFORM","ts":"1792800000.000100","text":"<@U0BOT> why is the ledger deploy stuck?"}}';
const START_RUN_ERROR =
  'start run: unexpected status from kora-agents: status 500: {"code":"internal","message":"failed to start run: sandbox pool exhausted"}';

const prReview = byName('review-opened-prs');
const ciFixer = byName('ci-failed-fixer');
const sentryTriage = byName('sentry-new-issue-triage');
const incident = byName('incident-summariser');
const linear = byName('linear-assign-to-coder');
const securitySweep = byName('weekly-security-sweep');
const depAudit = byName('nightly-dependency-audit');
const queueHealth = byName('queue-health-check');
const releaseNotes = byName('draft-release-notes');
const slackMention = byName('answer-platform-questions');
const flaky = byName('rerun-flaky-tests');

/** A pull request that fans out to two triggers, to show a multi-run delivery. */
const fanOut: DeliveryRow = (() => {
  const base = delivery(prReview, 9 * MIN, 'dispatched', { payloadExcerpt: PR_EXCERPT });
  const second: DeliveryRunRow = {
    triggerId: ciFixer.id,
    triggerName: ciFixer.name,
    runId: 'run_3f9c1ab04d',
    status: 'dispatched',
    error: null,
    createdAt: new Date(Date.parse(base.receivedAt) + 420).toISOString(),
  };
  return { ...base, payloadBytes: 18_240, runs: [...base.runs, second] };
})();

/** The newest fire in a coalesced burst: it ran, and replaced the one below. */
const COALESCED_WINNER = delivery(prReview, 2 * MIN, 'dispatched', {
  payloadExcerpt: PR_EXCERPT,
  payloadBytes: 17_902,
  coalesceKeys: [{ triggerId: prReview.id, key: 'acme/platform#1427', keyTooLong: false }],
});

/**
 * An earlier fire with the same key, replaced while it waited in the window. The
 * run is superseded and carries the reason; the delivery derives to skipped.
 */
const COALESCED_REPLACED = ((): DeliveryRow => {
  const base = delivery(prReview, 3 * MIN, 'superseded', {
    payloadExcerpt: PR_EXCERPT,
    payloadBytes: 17_880,
    coalesceKeys: [{ triggerId: prReview.id, key: 'acme/platform#1427', keyTooLong: false }],
  });
  return {
    ...base,
    status: 'skipped',
    runs: base.runs.map((r) => ({
      ...r,
      error: 'replaced by a newer fire with the same coalescing key',
      supersededBy: COALESCED_WINNER.id,
    })),
  };
})();

export const DELIVERIES: DeliveryRow[] = [
  COALESCED_WINNER,
  COALESCED_REPLACED,
  delivery(depAudit, 7 * MIN, 'dispatched', {
    scheduledAt: ago(7 * MIN),
    payloadBytes: 0,
  }),
  fanOut,
  delivery(sentryTriage, 14 * MIN, 'failed', {
    error: START_RUN_ERROR,
    payloadExcerpt: SENTRY_EXCERPT,
    payloadBytes: 6_104,
  }),
  delivery(ciFixer, 19 * MIN, 'dispatched', { payloadExcerpt: CI_EXCERPT, payloadBytes: 9_377 }),
  delivery(prReview, 26 * MIN, 'skipped', {
    reason: 'pull request is a draft',
    payloadExcerpt: PR_EXCERPT,
    payloadBytes: 17_410,
  }),
  delivery(releaseNotes, 31 * MIN, 'dispatched', { firedBy: 'usr_42b7e9', payloadBytes: 64 }),
  delivery(slackMention, 34 * MIN, 'dispatched', {
    payloadExcerpt: SLACK_EXCERPT,
    payloadBytes: 1_284,
  }),
  delivery(incident, 38 * MIN, 'denied', {
    error: 'policy: agent docs-writer is not permitted to read alerts:read',
    payloadBytes: 4_982,
  }),
  delivery(linear, 44 * MIN, 'dispatched', { payloadBytes: 3_218 }),
  delivery(prReview, 52 * MIN, 'duplicate', {
    reason: 'delivery id already processed',
    payloadExcerpt: PR_EXCERPT,
    payloadBytes: 17_902,
  }),
  delivery(sentryTriage, 71 * MIN, 'failed', { error: START_RUN_ERROR, payloadBytes: 5_870 }),
  delivery(queueHealth, 3 * HOUR + 12 * MIN, 'failed', {
    scheduledAt: ago(3 * HOUR + 12 * MIN),
    error: START_RUN_ERROR,
  }),
  delivery(securitySweep, 5 * HOUR, 'dispatched', { scheduledAt: ago(5 * HOUR) }),
  delivery(depAudit, 9 * HOUR, 'missed', {
    scheduledAt: ago(9 * HOUR),
    reason: 'platform was unavailable at the scheduled time',
  }),
  delivery(flaky, 11 * HOUR, 'dispatched', { firedBy: 'usr_42b7e9', payloadBytes: 92 }),
  delivery(incident, 13 * HOUR, 'failed', { error: START_RUN_ERROR, payloadBytes: 5_221 }),
  delivery(prReview, 15 * HOUR, 'dispatched', { payloadExcerpt: PR_EXCERPT, payloadBytes: 16_880 }),
  delivery(ciFixer, 19 * HOUR, 'superseded', {
    reason: 'a newer delivery replaced this one while it waited',
    payloadBytes: 9_100,
  }),
  delivery(linear, 22 * HOUR, 'dispatched', { payloadBytes: 3_340 }),
  delivery(prReview, 2 * DAY, 'dispatched', { payloadExcerpt: PR_EXCERPT, payloadBytes: 17_100 }),
  delivery(depAudit, 2 * DAY + 2 * HOUR, 'dispatched', { scheduledAt: ago(2 * DAY + 2 * HOUR) }),
];

/** A delivery past retention, whose payload has been purged. */
export const PURGED_DELIVERY: DeliveryRow = delivery(prReview, 40 * DAY, 'dispatched', {
  payloadPurged: true,
  payloadExcerpt: null,
  payloadBytes: 17_300,
});

/** The delivery the detail story opens: a failed webhook with a payload and a wrapped error chain. */
export const FAILED_DELIVERY: DeliveryRow =
  DELIVERIES.find((d) => d.status === 'failed') ?? PURGED_DELIVERY;

/** The base fleet plus two dozen repository triggers, to check the list at scale. */
export const MANY_TRIGGERS: TriggerRow[] = [
  ...TRIGGERS,
  ...Array.from({ length: 22 }, (_, i) => {
    const repos = [
      'payments',
      'ledger',
      'web',
      'mobile',
      'infra',
      'search',
      'billing',
      'auth',
      'notifications',
      'gateway',
      'analytics',
    ];
    const repo = repos[i % repos.length] ?? 'platform';
    const slug = i % 3 === 0 ? 'code-reviewer' : i % 3 === 1 ? 'coder' : 'security-reviewer';
    return trigger({
      id: `trg_${(0xd10000 + i * 7919).toString(16)}`,
      name: i % 2 === 0 ? `review-${repo}-prs` : `${repo}-push-checks`,
      kind: 'webhook',
      agentSlug: slug,
      sourceId: 'src_4d1f9a7c',
      eventType: i % 2 === 0 ? 'pull_request' : 'push',
      enabled: i % 7 !== 3,
      consecutiveFailures: i % 9 === 4 ? 1 : 0,
      createdAt: ago((3 + i) * DAY),
    });
  }),
];

/** The base deliveries plus forty more spread back through the day. */
export const MANY_DELIVERIES: DeliveryRow[] = [
  ...DELIVERIES,
  ...Array.from({ length: 40 }, (_, i) => {
    const t = MANY_TRIGGERS[i % MANY_TRIGGERS.length] ?? prReview;
    const status = i % 11 === 5 ? 'failed' : i % 8 === 3 ? 'skipped' : 'dispatched';
    return delivery(t, (3 + i * 29) * MIN, status, {
      ...(status === 'failed' ? { error: START_RUN_ERROR } : {}),
      payloadBytes: 4_000 + i * 311,
    });
  }),
];

// ── Catalog and preview answers the drawer and wizard fetch ──────────────────

export const GITHUB_EVENT_TYPES: TriggerEventTypes = {
  declared: [
    'pull_request',
    'pull_request_review',
    'push',
    'issues',
    'issue_comment',
    'check_run',
    'workflow_run',
    'release',
    'secret_scanning_alert',
  ],
  observed: ['pull_request', 'push', 'check_run', 'issue_comment'],
  category: 'webhook',
  senders: {
    roles: [
      { value: 'OWNER', label: 'Owner', description: 'Owns the repository' },
      { value: 'MEMBER', label: 'Member', description: 'Member of the organization' },
      { value: 'COLLABORATOR', label: 'Collaborator', description: 'Invited to the repository' },
    ],
  },
};

/** Alertmanager reports no sender, so its triggers have no sender restriction. */
export const ALERTMANAGER_EVENT_TYPES: TriggerEventTypes = {
  declared: ['alert.firing', 'alert.resolved'],
  category: 'webhook',
};

/** Every chat source reads the chat schema. Slack identifies senders by id and reports no roles. */
export const SLACK_EVENT_TYPES: TriggerEventTypes = {
  declared: ['mention', 'direct_message'],
  observed: ['mention'],
  category: 'chat',
  senders: { roles: [] },
};

/** The event types per provider, for the provider and source catalog routes. */
export const EVENT_TYPES_BY_PROVIDER: Record<string, TriggerEventTypes> = {
  github: GITHUB_EVENT_TYPES,
  alertmanager: ALERTMANAGER_EVENT_TYPES,
  slack: SLACK_EVENT_TYPES,
};

/** The fields of a chat event, from the chat schema, plus the meta fields. */
export const CHAT_FIELDS: TriggerField[] = [
  {
    path: 'text',
    group: 'event',
    description:
      'The message text as the platform sends it; empty for a message that carries only a file',
  },
  { path: 'platform', group: 'event', description: 'The chat platform, such as slack' },
  { path: 'user.id', group: 'user', description: "The platform's id for the sender" },
  {
    path: 'conversation.id',
    group: 'conversation',
    description: "The platform's id for the conversation",
  },
  {
    path: 'conversation.type',
    group: 'conversation',
    description: 'The kind of conversation: channel, private, dm or group',
  },
  {
    path: 'thread.id',
    group: 'thread',
    description: "The thread's id; a message outside a thread is its own thread",
  },
  {
    path: 'thread.started_by.id',
    group: 'thread',
    description: "The platform's id for the user who posted the thread's first message",
  },
  {
    path: 'message.id',
    group: 'message',
    description: "The platform's id for the message; it equals thread.id outside a thread and differs for a reply",
  },
  { path: 'workspace.id', group: 'workspace', description: "The platform's id for the workspace" },
  { path: 'meta.event_type', group: 'meta', description: 'The event type of the delivery' },
  { path: 'meta.delivery_id', group: 'meta', description: "The provider's ID for the delivery" },
  {
    path: 'meta.source',
    group: 'meta',
    description: 'The name of the source the delivery arrived on',
  },
];

export const PULL_REQUEST_FIELDS: TriggerField[] = [
  { path: 'payload.action', group: 'Event', enum: ['opened', 'reopened', 'closed', 'synchronize'] },
  { path: 'payload.number', group: 'Pull request', description: 'The pull request number.' },
  { path: 'payload.pull_request.title', group: 'Pull request' },
  { path: 'payload.pull_request.draft', group: 'Pull request', enum: ['true', 'false'] },
  { path: 'payload.pull_request.user.login', group: 'Pull request' },
  { path: 'payload.pull_request.base.repo.default_branch', group: 'Repository' },
  { path: 'payload.pull_request.head.repo.full_name', group: 'Repository' },
  { path: 'payload.repository.full_name', group: 'Repository' },
  { path: 'payload.sender.login', group: 'Sender' },
  { path: 'meta.event_type', group: 'Meta' },
];

export const SCHEDULE_PREVIEW: SchedulePreviewResponse = {
  timezone: 'UTC',
  next_fires: [
    { at: new Date(Date.now() + 6 * HOUR).toISOString(), label: 'Tomorrow at 02:00 UTC' },
    { at: new Date(Date.now() + 30 * HOUR).toISOString(), label: 'In 2 days at 02:00 UTC' },
    { at: new Date(Date.now() + 54 * HOUR).toISOString(), label: 'In 3 days at 02:00 UTC' },
    { at: new Date(Date.now() + 78 * HOUR).toISOString(), label: 'In 4 days at 02:00 UTC' },
  ],
};

// ── Routing ──────────────────────────────────────────────────────────────────
//
// What "Let kora choose" reads: the candidates and model catalog the wizard
// fetches, and routed triggers and deliveries mapped from their API shapes by
// the page's own functions.

const DESCRIPTIONS: Record<string, string> = {
  'code-reviewer':
    'Reviews pull requests for correctness, tests and readability, and leaves inline comments.',
  coder: 'Implements a change from an issue or a failing check and opens a pull request for it.',
  'security-reviewer':
    'Reviews changes that touch auth, secrets or dependencies, and flags security risks.',
  'docs-writer': 'Writes docs.',
  'triage-bot': 'Labels new issues, finds duplicates and assigns an owner from CODEOWNERS.',
};

const EXTRA_AGENTS: [string, string, string][] = [
  [
    'release-manager',
    'Release Manager',
    'Drafts release notes from merged pull requests and tags a release candidate.',
  ],
  [
    'dependency-bot',
    'Dependency Bot',
    'Upgrades dependencies, reads changelogs and opens one pull request per upgrade.',
  ],
  [
    'flaky-test-hunter',
    'Flaky Test Hunter',
    'Reruns failed checks, spots flaky tests and quarantines them with an issue.',
  ],
  [
    'incident-scribe',
    'Incident Scribe',
    'Summarises an incident channel into a timeline and a follow-up list.',
  ],
  [
    'perf-analyst',
    'Perf Analyst',
    'Profiles slow endpoints from a benchmark run and proposes a fix.',
  ],
  [
    'migration-checker',
    'Migration Checker',
    'Checks schema migrations for locks, backfills and rollback safety.',
  ],
];

/**
 * What GET /api/triggers/routing/candidates answers. `count` caps the runnable
 * agents; one agent without a run grant and the delegated agent always follow,
 * since both must show disabled.
 */
export function routingCandidates(
  count: number,
  descriptions: Record<string, string> = {},
): TriggerRoutingCandidate[] {
  const runnable: TriggerRoutingCandidate[] = [
    ...AGENTS.filter((a) => a.identityMode !== 'delegated').map((a) => ({
      id: a.id,
      slug: a.slug,
      name: a.name,
      description: descriptions[a.slug] ?? DESCRIPTIONS[a.slug] ?? '',
      identity_mode: 'autonomous' as const,
      runnable: true,
    })),
    ...EXTRA_AGENTS.map(([slug, name, description], i) => ({
      id: `at_r${String(i).padStart(2, '0')}`,
      slug,
      name,
      description: descriptions[slug] ?? description,
      identity_mode: 'autonomous' as const,
      runnable: true,
    })),
  ];
  const blocked: TriggerRoutingCandidate = {
    id: 'at_noperm01',
    slug: 'billing-agent',
    name: 'Billing Agent',
    description: 'Reconciles invoices against usage and raises disputes with the vendor.',
    identity_mode: 'autonomous',
    runnable: false,
    reason: 'you do not hold agent:billing-agent:run',
  };
  const delegated: TriggerRoutingCandidate[] = AGENTS.filter(
    (a) => a.identityMode === 'delegated',
  ).map((a) => ({
    id: a.id,
    slug: a.slug,
    name: a.name,
    description: 'Works through your inbox and calendar as you.',
    identity_mode: 'delegated',
    runnable: false,
    reason: 'delegated agents act for the person who runs them, so a trigger cannot start them',
  }));
  return [...runnable.slice(0, count), blocked, ...delegated];
}

function catalogModel(id: string, name: string, capability: 'decision' | 'chat'): ModelObject {
  return {
    id,
    object: 'model',
    created: 1_758_000_000,
    owned_by: id.split('/')[0] ?? id,
    family: id.split('/')[0] ?? id,
    display_name: name,
    status: 'available',
    aliases: [],
    input_modalities: ['text'],
    output_modalities: ['text'],
    dialects: capability === 'chat' ? ['openai', 'anthropic'] : [],
    capabilities: [capability],
  };
}

const CHAT_MODELS: ModelObject[] = [
  catalogModel('anthropic/claude-sonnet-4-6', 'Claude Sonnet 4.6', 'chat'),
  catalogModel('anthropic/claude-opus-4-1', 'Claude Opus 4.1', 'chat'),
  catalogModel('openai/gpt-5', 'GPT-5', 'chat'),
];

const DECISION_MODELS: ModelObject[] = [
  catalogModel('typesafe/jev-1-14', 'Jev 1.14', 'decision'),
  catalogModel('cloudflare/clef', 'Clef', 'decision'),
  catalogModel('cloudflare/clef-flash', 'Clef Flash', 'decision'),
  catalogModel('cloudflare/clef-omni', 'Clef Omni', 'decision'),
  catalogModel('liquid/d1', 'Liquid d1', 'decision'),
  catalogModel('liquid/d1-ultra', 'Liquid d1 Ultra', 'decision'),
  catalogModel('upstage/solar-decide', 'Solar Decide', 'decision'),
  catalogModel('upstage/solar-decide-flash', 'Solar Decide Flash', 'decision'),
  catalogModel('perplexity/pplx-decider-v1-2', 'Perplexity Decider V1.2', 'decision'),
  catalogModel('perplexity/pplx-decider-flash-v1-2', 'Perplexity Decider Flash V1.2', 'decision'),
];

/** What GET /api/models answers: chat models the picker must drop, mixed with the decision models. */
export function modelCatalog(withDecisionModels: boolean): ModelObject[] {
  return withDecisionModels
    ? [
        ...CHAT_MODELS.slice(0, 1),
        ...DECISION_MODELS.slice(0, 5),
        ...CHAT_MODELS.slice(1, 2),
        ...DECISION_MODELS.slice(5),
        ...CHAT_MODELS.slice(2),
      ]
    : CHAT_MODELS;
}

const ROUTED_NAME = 'route-github-work';

/** A webhook trigger that lets kora choose between four agents, the triage bot as fallback. */
export const ROUTED_TRIGGER: TriggerRow = toTriggerRow({
  id: 'trg_9e41d7a0',
  tenant_id: 'tnt_acme',
  kind: 'webhook',
  source_id: 'src_4d1f9a7c',
  name: ROUTED_NAME,
  target_kind: 'routed',
  agent_type_id: '',
  agent_slug: '',
  event_type: 'issue_comment',
  prompt_template: 'Handle this comment on {{ issue.html_url }}: {{ comment.body }}',
  routing: {
    candidates: [
      agent('code-reviewer').id,
      agent('coder').id,
      agent('security-reviewer').id,
      agent('triage-bot').id,
    ],
    model: 'liquid/d1',
    confidence: 'balanced',
    fallback_agent_type_id: agent('triage-bot').id,
    join_runs: true,
    join_key: '{{ repository.full_name }}#{{ issue.number }}',
  },
  enabled: true,
  created_at: ago(6 * DAY),
  updated_at: ago(6 * DAY),
});

export type RoutingCase = 'confident' | 'joined-by-key' | 'fallback' | 'no-run' | 'model-failed';

const scores = (pairs: [string, number][]) => ({
  agents: Object.fromEntries(pairs.map(([slug, p]) => [agent(slug).id, p])),
});

const CONFIDENT: TriggerRoutingDecision = {
  outcome: 'new_run',
  key_matched: false,
  join_key: 'acme/platform#1427',
  model: 'liquid/d1',
  agent_type_id: agent('code-reviewer').id,
  probability: 0.91,
  threshold: 0.7,
  scores: scores([
    ['code-reviewer', 0.91],
    ['security-reviewer', 0.06],
    ['coder', 0.03],
  ]),
  fallback_reason: null,
  error: null,
};

const UNSURE = scores([
  ['coder', 0.48],
  ['code-reviewer', 0.37],
  ['security-reviewer', 0.15],
]);

const ROUTING_CASES: Record<
  RoutingCase,
  { status: string; runId?: string; error?: string; routing: TriggerRoutingDecision }
> = {
  confident: { status: 'dispatched', runId: 'run_7c21e4b2', routing: CONFIDENT },
  'joined-by-key': {
    status: 'joined',
    runId: 'run_3f9c1ab04d',
    routing: {
      ...CONFIDENT,
      outcome: 'joined_run',
      key_matched: true,
      model: null,
      probability: null,
      threshold: null,
      scores: {},
    },
  },
  fallback: {
    status: 'dispatched',
    runId: 'run_a90d33b2',
    routing: {
      ...CONFIDENT,
      outcome: 'fallback_run',
      agent_type_id: agent('triage-bot').id,
      probability: 0.48,
      scores: UNSURE,
      fallback_reason: 'below_threshold',
    },
  },
  'no-run': {
    status: 'unrouted',
    error: 'kora was not sure which agent fits, and the trigger has no fallback agent',
    routing: {
      ...CONFIDENT,
      outcome: 'no_run',
      agent_type_id: null,
      probability: 0.48,
      scores: UNSURE,
      fallback_reason: 'below_threshold',
    },
  },
  'model-failed': {
    status: 'dispatched',
    runId: 'run_51be07b2',
    routing: {
      ...CONFIDENT,
      outcome: 'fallback_run',
      agent_type_id: agent('triage-bot').id,
      probability: null,
      threshold: 0.7,
      scores: {},
      fallback_reason: 'decision_failed',
      error: 'decision model: liquid/d1: context deadline exceeded after 10s',
    },
  },
};

/** One delivery of the routed trigger and the decision recorded for it, as the API returns it. */
export function routedDelivery(c: RoutingCase): DeliveryRow {
  const { status, runId, error, routing } = ROUTING_CASES[c];
  const receivedAt = ago(4 * MIN);
  const wire: TriggerDelivery = {
    id: `dlv_routed_${c.replace(/-/g, '_')}`,
    tenant_id: 'tnt_acme',
    kind: 'webhook',
    source_id: ROUTED_TRIGGER.sourceId,
    trigger_id: ROUTED_TRIGGER.id,
    trigger_name: ROUTED_NAME,
    event_type: 'issue_comment',
    status: status === 'unrouted' ? 'skipped' : 'dispatched',
    ...(runId !== undefined && { run_id: runId }),
    payload_excerpt:
      '{"action":"created","issue":{"number":1427},"comment":{"body":"Can someone check the auth change?"}}',
    payload_bytes: 14_220,
    received_at: receivedAt,
    runs: [
      {
        id: `dr_${c}`,
        delivery_id: `dlv_routed_${c}`,
        trigger_id: ROUTED_TRIGGER.id,
        trigger_name: ROUTED_NAME,
        status,
        ...(runId !== undefined && { run_id: runId }),
        ...(error !== undefined && { error }),
        routing,
        created_at: new Date(Date.parse(receivedAt) + 640).toISOString(),
      },
    ],
  };
  return toDeliveryRow(wire);
}
