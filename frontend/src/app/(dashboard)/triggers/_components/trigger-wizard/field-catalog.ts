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
  { token: 'event', description: 'The event type, e.g. pull_request', example: 'pull_request' },
  { token: 'action', description: "The event's action, e.g. opened", example: 'opened' },
  {
    token: 'repo.full_name',
    description: 'Owner and repository, e.g. acme/widgets',
    example: 'acme/widgets',
  },
  { token: 'repo.default_branch', description: "The repository's default branch", example: 'main' },
  {
    token: 'sender.login',
    description: 'The GitHub user who caused the event',
    example: 'jwilkin',
  },
];

/** GitHub fields that exist only on particular events. */
const GITHUB_PER_EVENT: FieldCatalogEntry[] = [
  { token: 'pr.number', description: 'The PR number', events: ['pull_request'], example: '108' },
  {
    token: 'pr.title',
    description: "The PR's title",
    events: ['pull_request'],
    example: 'Fix login retry',
  },
  {
    token: 'pr.url',
    description: 'Link to the PR',
    events: ['pull_request'],
    example: 'https://github.com/acme/widgets/pull/108',
  },
  {
    token: 'pr.head_ref',
    description: 'Branch the PR is from',
    events: ['pull_request'],
    example: 'fix/login-retry',
  },
  {
    token: 'pr.head_sha',
    description: 'Commit at the head of the PR',
    events: ['pull_request'],
    example: '4a6565c',
  },
  {
    token: 'pr.base_ref',
    description: 'Branch the PR targets',
    events: ['pull_request'],
    example: 'main',
  },
  {
    token: 'pr.author',
    description: 'Who opened the PR',
    events: ['pull_request'],
    example: 'jwilkin',
  },

  {
    token: 'push.ref',
    description: 'Full ref pushed, e.g. refs/heads/main',
    events: ['push'],
    example: 'refs/heads/main',
  },
  { token: 'push.branch', description: 'Branch pushed to', events: ['push'], example: 'main' },
  {
    token: 'push.before',
    description: 'Commit before the push',
    events: ['push'],
    example: '81ff7b1',
  },
  {
    token: 'push.after',
    description: 'Commit after the push',
    events: ['push'],
    example: '30f1bd0',
  },
  {
    token: 'push.commit_message',
    description: 'Message of the head commit',
    events: ['push'],
    example: 'fix: retry logins',
  },

  { token: 'issue.number', description: 'The issue number', events: ['issues'], example: '42' },
  {
    token: 'issue.title',
    description: "The issue's title",
    events: ['issues'],
    example: 'Login retries fail',
  },
  { token: 'issue.body', description: "The issue's body", events: ['issues'] },
  {
    token: 'issue.url',
    description: 'Link to the issue',
    events: ['issues'],
    example: 'https://github.com/acme/widgets/issues/42',
  },
  {
    token: 'issue.author',
    description: 'Who opened the issue',
    events: ['issues'],
    example: 'jwilkin',
  },
  {
    token: 'label.name',
    description: 'The label added or removed, on labeled and unlabeled',
    events: ['issues'],
  },
];

/** Alertmanager fields, all present on every delivery. */
const ALERTMANAGER: FieldCatalogEntry[] = [
  { token: 'event', description: 'firing or resolved' },
  { token: 'status', description: "The group's status" },
  { token: 'receiver', description: 'Alertmanager receiver that routed it' },
  { token: 'group_key', description: 'Identifies the alert group' },
  { token: 'external_url', description: 'Link back to Alertmanager' },
  { token: 'alerts.count', description: 'How many alerts are in the group' },
  { token: 'first_alert.status', description: "The first alert's status" },
  { token: 'first_alert.starts_at', description: 'When the first alert started' },
  { token: 'first_alert.ends_at', description: 'When the first alert ended' },
  { token: 'first_alert.generator_url', description: 'Link to the rule that fired' },
  { token: 'first_alert.fingerprint', description: "The first alert's fingerprint" },
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
  { token: 'var.date', description: 'Fire date, e.g. 2026-07-28', example: '2026-07-28' },
  {
    token: 'var.datetime',
    description: 'Fire time, RFC 3339',
    example: '2026-07-28T02:00:00+10:00',
  },
  { token: 'var.trigger_id', description: "This trigger's id" },
  { token: 'var.tenant_id', description: 'The tenant the run belongs to' },
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
    ...entries.map((e) => ({ token: e.token, description: e.description, example: e.example })),
    ...patterns.map((token) => ({
      token,
      description: 'Named by your Prometheus rules',
      pattern: true,
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
