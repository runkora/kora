import type { SchedulePreviewResponse, TriggerEventTypes, TriggerField } from '@orbit/shared/api';
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
  { id: 'at_8f3a1c92', slug: 'code-reviewer', name: 'Code Reviewer', identityMode: 'autonomous' },
  { id: 'at_21b7d4e0', slug: 'coder', name: 'Coder', identityMode: 'autonomous' },
  {
    id: 'at_5c90aa17',
    slug: 'security-reviewer',
    name: 'Security Reviewer',
    identityMode: 'autonomous',
  },
  { id: 'at_77e2f0b3', slug: 'docs-writer', name: 'Docs Writer', identityMode: 'autonomous' },
  { id: 'at_09d6c5a8', slug: 'triage-bot', name: 'Triage Bot', identityMode: 'autonomous' },
  {
    id: 'at_c41e8b6d',
    slug: 'personal-assistant',
    name: 'Personal Assistant',
    identityMode: 'delegated',
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
    enabled: true,
    createdAt: ago(41 * DAY),
    rejectionCount: 0,
    lastRejectionAt: null,
    lastRejectionReason: null,
  },
  {
    id: 'src_b82e3016',
    name: 'sentry-prod',
    provider: 'sentry',
    enabled: true,
    createdAt: ago(27 * DAY),
    rejectionCount: 3,
    lastRejectionAt: ago(2 * HOUR),
    lastRejectionReason: 'signature mismatch',
  },
  {
    id: 'src_0a6c57de',
    name: 'linear-workspace',
    provider: 'linear',
    enabled: true,
    createdAt: ago(19 * DAY),
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
    agentTypeId: owner.id,
    eventType: '',
    matchCondition: {},
    matchContains: {},
    senderRestriction: { logins: [], associations: [] },
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
  senderRestriction: { logins: ['dependabot[bot]', 'jwilkin'], associations: ['MEMBER', 'OWNER'] },
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

export const TRIGGERS: TriggerRow[] = [
  PR_REVIEW_TRIGGER,
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

export const DELIVERIES: DeliveryRow[] = [
  delivery(prReview, 2 * MIN, 'dispatched', {
    payloadExcerpt: PR_EXCERPT,
    payloadBytes: 17_902,
    coalesceKeys: [{ triggerId: prReview.id, key: 'acme/platform#1427', keyTooLong: false }],
  }),
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
export const FAILED_DELIVERY: DeliveryRow = DELIVERIES[3] ?? PURGED_DELIVERY;

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
  associations: ['OWNER', 'MEMBER', 'COLLABORATOR', 'CONTRIBUTOR', 'NONE'],
};

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
