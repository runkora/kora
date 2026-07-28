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
import type { FieldCatalogEntry, WizardField } from './types';

/** GitHub fields present on every event. */
const GITHUB_COMMON: FieldCatalogEntry[] = [
  { token: 'event', description: 'The event type, e.g. pull_request' },
  { token: 'action', description: "The event's action, e.g. opened" },
  { token: 'repo.full_name', description: 'Owner and repository, e.g. acme/widgets' },
  { token: 'repo.default_branch', description: "The repository's default branch" },
  { token: 'sender.login', description: 'The GitHub user who caused the event' },
];

/** GitHub fields that exist only on particular events. */
const GITHUB_PER_EVENT: FieldCatalogEntry[] = [
  { token: 'pr.number', description: 'The PR number', events: ['pull_request'] },
  { token: 'pr.title', description: "The PR's title", events: ['pull_request'] },
  { token: 'pr.url', description: 'Link to the PR', events: ['pull_request'] },
  { token: 'pr.head_ref', description: 'Branch the PR is from', events: ['pull_request'] },
  { token: 'pr.head_sha', description: 'Commit at the head of the PR', events: ['pull_request'] },
  { token: 'pr.base_ref', description: 'Branch the PR targets', events: ['pull_request'] },
  { token: 'pr.author', description: 'Who opened the PR', events: ['pull_request'] },

  { token: 'push.ref', description: 'Full ref pushed, e.g. refs/heads/main', events: ['push'] },
  { token: 'push.branch', description: 'Branch pushed to', events: ['push'] },
  { token: 'push.before', description: 'Commit before the push', events: ['push'] },
  { token: 'push.after', description: 'Commit after the push', events: ['push'] },
  { token: 'push.commit_message', description: 'Message of the head commit', events: ['push'] },

  { token: 'issue.number', description: 'The issue number', events: ['issues'] },
  { token: 'issue.title', description: "The issue's title", events: ['issues'] },
  { token: 'issue.body', description: "The issue's body", events: ['issues'] },
  { token: 'issue.url', description: 'Link to the issue', events: ['issues'] },
  { token: 'issue.author', description: 'Who opened the issue', events: ['issues'] },
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
  { token: 'var.date', description: 'Fire date, e.g. 2026-07-28' },
  { token: 'var.datetime', description: 'Fire time, RFC 3339' },
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
    }));
}

/** Pattern hints for a provider, shown but not insertable. */
export function catalogPatterns(provider: string): string[] {
  return provider === 'alertmanager' ? ALERTMANAGER_PATTERNS : [];
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
