/**
 * @tier 3 — triggers-scoped. The delivery fields a trigger can read.
 *
 * The backend extracts one allow-listed field map per delivery, and that same
 * map is what `match_condition` filters on and what `{{ dotted.field }}` prompt
 * placeholders render from. One catalog therefore drives both the filter picker
 * and the prompt builder: if they disagreed, one of them would be lying.
 *
 * The vocabulary mirrors `backend/internal/triggers/adapter_github.go`,
 * `adapter_alertmanager.go`, and the scheduler's injected vars in `service.go`.
 * Nothing here is invented — a token absent from those files never resolves.
 */

import { deriveFieldKey } from './wizard-state';
import type { TemplateCatalogEntry } from '@orbit/terra';
import type { FieldCatalogEntry, WizardField } from './types';

/** GitHub fields present on every event. */
const GITHUB_COMMON: FieldCatalogEntry[] = [
  {
    token: 'event',
    description: 'The event type, e.g. pull_request',
    example: 'pull_request',
    group: 'Common',
  },
  {
    token: 'action',
    description: "The event's action, e.g. opened",
    example: 'opened',
    group: 'Common',
  },
  {
    token: 'repo.full_name',
    description: 'Owner and repository, e.g. my-org/my-repo',
    example: 'my-org/my-repo',
    group: 'Common',
  },
  {
    token: 'repo.default_branch',
    description: "The repository's default branch",
    example: 'main',
    group: 'Common',
  },
  {
    token: 'sender.login',
    description: 'The GitHub user who caused the event',
    example: 'some-user',
    group: 'Common',
  },
];

/** GitHub fields that exist only on particular events. */
const GITHUB_PER_EVENT: FieldCatalogEntry[] = [
  {
    token: 'pr.number',
    description: 'The PR number',
    events: ['pull_request'],
    example: '108',
    group: 'Pull request',
  },
  {
    token: 'pr.title',
    description: "The PR's title",
    events: ['pull_request'],
    example: 'Fix login retry',
    group: 'Pull request',
  },
  {
    token: 'pr.url',
    description: 'Link to the PR',
    events: ['pull_request'],
    example: 'https://github.com/my-org/my-repo/pull/108',
    group: 'Pull request',
  },
  {
    token: 'pr.head_ref',
    description: 'Branch the PR is from',
    events: ['pull_request'],
    example: 'fix/login-retry',
    group: 'Pull request',
  },
  {
    token: 'pr.head_sha',
    description: 'Commit at the head of the PR',
    events: ['pull_request'],
    example: '4a6565c',
    group: 'Pull request',
  },
  {
    token: 'pr.base_ref',
    description: 'Branch the PR targets',
    events: ['pull_request'],
    example: 'main',
    group: 'Pull request',
  },
  {
    token: 'pr.author',
    description: 'Who opened the PR',
    events: ['pull_request'],
    example: 'some-user',
    group: 'Pull request',
  },

  {
    token: 'push.ref',
    description: 'Full ref pushed, e.g. refs/heads/main',
    events: ['push'],
    example: 'refs/heads/main',
    group: 'Push',
  },
  {
    token: 'push.branch',
    description: 'Branch pushed to',
    events: ['push'],
    example: 'main',
    group: 'Push',
  },
  {
    token: 'push.before',
    description: 'Commit before the push',
    events: ['push'],
    example: '81ff7b1',
    group: 'Push',
  },
  {
    token: 'push.after',
    description: 'Commit after the push',
    events: ['push'],
    example: '30f1bd0',
    group: 'Push',
  },
  {
    token: 'push.commit_message',
    description: 'Message of the head commit',
    events: ['push'],
    example: 'fix: retry logins',
    group: 'Push',
  },

  {
    token: 'issue.number',
    description: 'The issue number',
    events: ['issues'],
    example: '42',
    group: 'Issues',
  },
  {
    token: 'issue.title',
    description: "The issue's title",
    events: ['issues'],
    example: 'Login retries fail',
    group: 'Issues',
  },
  { token: 'issue.body', description: "The issue's body", events: ['issues'], group: 'Issues' },
  {
    token: 'issue.url',
    description: 'Link to the issue',
    events: ['issues'],
    example: 'https://github.com/my-org/my-repo/issues/42',
    group: 'Issues',
  },
  {
    token: 'issue.author',
    description: 'Who opened the issue',
    events: ['issues'],
    example: 'some-user',
    group: 'Issues',
  },
  {
    token: 'label.name',
    description: 'The label added or removed, on labeled and unlabeled',
    events: ['issues'],
    group: 'Issues',
  },
];

/** Alertmanager fields, all present on every delivery. */
const ALERTMANAGER: FieldCatalogEntry[] = [
  { token: 'event', description: 'firing or resolved', group: 'Alert group' },
  { token: 'status', description: "The group's status", group: 'Alert group' },
  { token: 'receiver', description: 'Alertmanager receiver that routed it', group: 'Alert group' },
  { token: 'group_key', description: 'Identifies the alert group', group: 'Alert group' },
  { token: 'external_url', description: 'Link back to Alertmanager', group: 'Alert group' },
  { token: 'alerts.count', description: 'How many alerts are in the group', group: 'Alert group' },
  { token: 'first_alert.status', description: "The first alert's status", group: 'First alert' },
  {
    token: 'first_alert.starts_at',
    description: 'When the first alert started',
    group: 'First alert',
  },
  { token: 'first_alert.ends_at', description: 'When the first alert ended', group: 'First alert' },
  {
    token: 'first_alert.generator_url',
    description: 'Link to the rule that fired',
    group: 'First alert',
  },
  {
    token: 'first_alert.fingerprint',
    description: "The first alert's fingerprint",
    group: 'First alert',
  },
];

/**
 * Alertmanager also exposes every label and annotation by name. The names come
 * from the Prometheus rules, so they cannot be enumerated here — they are shown
 * as patterns rather than offered as chips.
 */
export const ALERTMANAGER_PATTERNS: string[] = [
  'group.labels.<name>',
  'common.labels.<name>',
  'common.annotations.<name>',
  'first_alert.labels.<name>',
  'first_alert.annotations.<name>',
];

/** The scheduler injects these on every fire. */
const SCHEDULED: FieldCatalogEntry[] = [
  {
    token: 'var.date',
    description: 'Fire date, e.g. 2026-07-28',
    example: '2026-07-28',
    group: 'Schedule',
  },
  {
    token: 'var.datetime',
    description: 'Fire time, RFC 3339',
    example: '2026-07-28T02:00:00+10:00',
    group: 'Schedule',
  },
  { token: 'var.trigger_id', description: "This trigger's id", group: 'Schedule' },
  { token: 'var.tenant_id', description: 'The tenant the run belongs to', group: 'Schedule' },
];

/**
 * The fields a webhook delivery carries, narrowed to one event when one is
 * chosen. On "any event" the per-event fields are still listed: a trigger that
 * matches everything can still read them, they are simply empty when the
 * delivery is a different event.
 */
export function webhookCatalog(provider: string, eventType: string): FieldCatalogEntry[] {
  if (provider === 'alertmanager') return ALERTMANAGER;
  if (provider !== 'github') return [];
  if (!eventType) return [...GITHUB_COMMON, ...GITHUB_PER_EVENT];
  return [
    ...GITHUB_COMMON,
    ...GITHUB_PER_EVENT.filter((f) => !f.events || f.events.includes(eventType)),
  ];
}

/** The fields a scheduled fire carries. */
export function scheduledCatalog(): FieldCatalogEntry[] {
  return SCHEDULED;
}

/** The fields a manual fire carries: one per declared input. */
export function manualCatalog(fields: WizardField[]): FieldCatalogEntry[] {
  return fields
    .filter((f) => f.label.trim())
    .map((f, i) => ({
      token: `var.${deriveFieldKey(f.label, i)}`,
      description: `The "${f.label.trim()}" input`,
      group: 'Inputs',
      // A sample in the field's own type, so the preview reads plausibly.
      example:
        f.type === 'number'
          ? '3'
          : f.type === 'boolean'
            ? 'true'
            : `a ${f.label.trim().toLowerCase()}`,
    }));
}

/** Pattern hints for a provider, shown but not insertable. */
export function catalogPatterns(provider: string): string[] {
  return provider === 'alertmanager' ? ALERTMANAGER_PATTERNS : [];
}

/**
 * Adapts this wizard's catalog to Terra's TemplateField, appending any dynamic
 * pattern families as listed-but-not-insertable entries.
 */
export function toTemplateCatalog(
  entries: FieldCatalogEntry[],
  patterns: string[] = [],
): TemplateCatalogEntry[] {
  return [
    ...entries.map((e) => ({
      token: e.token,
      description: e.description,
      example: e.example,
      group: e.group,
    })),
    ...patterns.map((token) => ({
      token,
      description: 'Named by your Prometheus rules',
      pattern: true,
      group: 'Dynamic labels',
    })),
  ];
}

/**
 * Inserts a `{{ token }}` placeholder at the cursor.
 *
 * A null cursor means the textarea was never focused, so the placeholder is
 * appended — clicking a field should always do something, and appending is the
 * one guess that never destroys what was typed.
 */
export function insertAtCursor(text: string, cursor: number | null, token: string): string {
  const placeholder = `{{ ${token} }}`;
  if (cursor === null || cursor < 0 || cursor > text.length) {
    if (!text) return placeholder;
    return text.endsWith(' ') ? `${text}${placeholder}` : `${text} ${placeholder}`;
  }
  return `${text.slice(0, cursor)}${placeholder}${text.slice(cursor)}`;
}
