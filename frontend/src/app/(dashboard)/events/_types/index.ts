/**
 * @tier 3 — events-scoped. The page's tabs and filters, the event catalog and the display
 * helpers for one event: its summary, tone, resource, principal and the links out of it.
 */

import type { EventPrincipalType, KoraEvent } from '@runkora/shared/api';

export type { EventPage, EventPrincipalType, EventRecord, KoraEvent } from '@runkora/shared/api';

export type EventTab =
  | 'all'
  | 'runs'
  | 'sandboxes'
  | 'agents'
  | 'environments'
  | 'triggers'
  | 'budgets'
  | 'identity'
  | 'policy'
  | 'declared';

/** Each tab and the `<domain>.<entity>` categories it lists. */
export const TABS: { value: EventTab; label: string; categories: string[] }[] = [
  { value: 'all', label: 'All', categories: [] },
  { value: 'runs', label: 'Runs', categories: ['agents.run'] },
  { value: 'sandboxes', label: 'Sandboxes', categories: ['agents.sandbox'] },
  { value: 'agents', label: 'Agents', categories: ['agents.agent'] },
  { value: 'environments', label: 'Environments', categories: ['agents.environment'] },
  {
    value: 'triggers',
    label: 'Triggers',
    categories: ['triggers.trigger', 'triggers.source', 'triggers.delivery'],
  },
  { value: 'budgets', label: 'Budgets', categories: ['inference.budget', 'policy.tenant_budget'] },
  {
    value: 'identity',
    label: 'Identity',
    categories: ['identity.session', 'identity.pat', 'identity.delegation', 'identity.tenant'],
  },
  { value: 'policy', label: 'Policy', categories: ['policy.grant', 'policy.role'] },
  { value: 'declared', label: 'Declared', categories: ['declarative.resource'] },
];

export function tabCategories(tab: EventTab): string[] {
  return TABS.find((t) => t.value === tab)?.categories ?? [];
}

/** The `<domain>.<entity>` category of a type: `agents.run` for `agents.run.failed.v1`. */
export function categoryOf(type: string): string {
  return type.split('.').slice(0, 2).join('.');
}

/** The tab an event belongs to, for the detail's status strip. */
export function tabLabelOf(type: string): string {
  const category = categoryOf(type);
  return TABS.find((t) => t.categories.includes(category))?.label ?? category;
}

const verbs = (entity: string, list: string[]) => list.map((v) => `${entity}.${v}.v1`);

/** The v1 catalog (docs/prd/event-spine.md), for the Event column filter. */
export const EVENT_CATALOG: string[] = [
  ...verbs('agents.run', [
    'created',
    'started',
    'completed',
    'failed',
    'budget_exhausted',
    'terminated',
    'idle',
    'interrupted',
    'errored',
    'guardrail_warned',
    'guardrail_paused',
    'message_received',
  ]),
  ...verbs('agents.sandbox', ['created', 'replaced', 'ready', 'failed', 'deleted']),
  ...verbs('agents.agent', ['created', 'updated', 'deleted']),
  ...verbs('agents.environment', ['created', 'updated', 'deleted']),
  ...verbs('triggers.delivery', ['failed', 'denied']),
  ...verbs('triggers.trigger', ['autopaused', 'created', 'updated', 'deleted']),
  ...verbs('triggers.source', ['created', 'updated', 'deleted']),
  ...verbs('inference.budget', ['threshold_crossed', 'exceeded']),
  ...verbs('identity.session', ['created']),
  ...verbs('identity.pat', ['created', 'revoked', 'updated']),
  ...verbs('identity.delegation', ['issued']),
  ...verbs('identity.tenant', ['switched']),
  ...verbs('policy.grant', ['created', 'updated', 'deleted']),
  ...verbs('policy.role', ['created', 'updated', 'deleted']),
  ...verbs('policy.tenant_budget', ['created', 'updated', 'deleted']),
  ...verbs('declarative.resource', ['failed']),
];

/** Events asked for on each page. Here, not in the hook, so the server page can read it. */
export const EVENT_PAGE_SIZE = 50;

/** What the page asks the API for. Every list matches any of its values. */
export interface EventFilters {
  tab: EventTab;
  types: string[];
  agents: string[];
  principals: string[];
  /** Search text; a search pauses live updates. */
  query: string;
}

export const NO_FILTERS: EventFilters = {
  tab: 'all',
  types: [],
  agents: [],
  principals: [],
  query: '',
};

export function isFiltered(f: EventFilters): boolean {
  return (
    f.tab !== 'all' ||
    f.types.length > 0 ||
    f.agents.length > 0 ||
    f.principals.length > 0 ||
    f.query.trim() !== ''
  );
}

/** The filters as `GET /v1/events` and the stream take them. The stream ignores `q`. */
export function filterParams(f: EventFilters): URLSearchParams {
  const params = new URLSearchParams();
  tabCategories(f.tab).forEach((c) => params.append('category', c));
  f.types.forEach((t) => params.append('type', t));
  f.agents.forEach((a) => params.append('agent', a));
  f.principals.forEach((p) => params.append('principal', p));
  if (f.query.trim() !== '') params.set('q', f.query.trim());
  return params;
}

/** The type without its version suffix: `agents.run.failed`. */
export function bareType(type: string): string {
  return type.replace(/\.v\d+$/, '');
}

export type EventTone = 'error' | 'degraded' | 'healthy' | 'muted';

/** Failures read red, warnings amber, finished work green, everything else neutral. */
export function eventTone(type: string): EventTone {
  const verb = bareType(type).split('.').pop() ?? '';
  if (['failed', 'denied', 'budget_exhausted', 'exceeded', 'autopaused', 'errored'].includes(verb))
    return 'error';
  if (['guardrail_warned', 'guardrail_paused', 'threshold_crossed', 'terminated'].includes(verb))
    return 'degraded';
  if (['completed', 'ready'].includes(verb)) return 'healthy';
  return 'muted';
}

const str = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined);

const ENTITY_NOUN: Record<string, string> = {
  'policy.grant': 'Grant',
  'policy.role': 'Role',
  'policy.tenant_budget': 'Tenant budget',
  'agents.agent': 'Agent',
  'agents.environment': 'Environment',
  'triggers.trigger': 'Trigger',
  'triggers.source': 'Source',
};

/** One short sentence saying what happened, without the principal. */
export function eventSummary(e: KoraEvent): string {
  const bare = bareType(e.type);
  const entity = e.data.entity;
  const error = str(entity.error) ?? str(entity.reason);
  const name = str(entity.name);
  switch (bare) {
    case 'agents.run.created':
      return 'Run queued';
    case 'agents.run.started':
      return 'Run started';
    case 'agents.run.completed':
      return 'Run completed';
    case 'agents.run.failed':
      return error ? `Run failed: ${error}` : 'Run failed';
    case 'agents.run.budget_exhausted':
      return 'Budget ran out and stopped the run';
    case 'agents.run.terminated':
      return 'Run terminated';
    case 'agents.run.idle':
      return 'Run is waiting for input';
    case 'agents.run.interrupted':
      return 'Turn interrupted';
    case 'agents.run.errored':
      return error ? `Turn ended in an error: ${error}` : 'Turn ended in an error';
    case 'agents.run.guardrail_warned':
      return 'Loop guard warned the agent';
    case 'agents.run.guardrail_paused':
      return 'Loop guard paused the run';
    case 'agents.run.message_received':
      return 'Run received a message';
    case 'agents.sandbox.created':
      return 'Sandbox provisioning';
    case 'agents.sandbox.replaced':
      return 'Sandbox replaced';
    case 'agents.sandbox.ready':
      return str(entity.egress) === 'skipped'
        ? 'Sandbox ready, egress check skipped'
        : 'Sandbox ready';
    case 'agents.sandbox.failed':
      return error ? `Sandbox failed: ${error}` : 'Sandbox failed to provision';
    case 'agents.sandbox.deleted':
      return 'Sandbox removed';
    case 'triggers.delivery.failed':
      return error
        ? `Delivery could not start its run: ${error}`
        : 'Delivery could not start its run';
    case 'triggers.delivery.denied':
      return 'Delivery refused for lack of permission';
    case 'triggers.trigger.autopaused':
      return `${name ?? 'Trigger'} paused itself after repeated failures`;
    case 'inference.budget.threshold_crossed':
      return `Budget passed ${String(entity.level ?? '')}% of its limit`;
    case 'inference.budget.exceeded':
      return 'Exhausted budget refused a request';
    case 'identity.session.created':
      return e.koraprincipaltype === 'AGENT' ? 'Agent session started' : 'Signed in';
    case 'identity.pat.created':
      return `Access token issued${name ? `: ${name}` : ''}`;
    case 'identity.pat.revoked':
      return `Access token revoked${name ? `: ${name}` : ''}`;
    case 'identity.pat.updated':
      return `Access token scopes changed${name ? `: ${name}` : ''}`;
    case 'identity.delegation.issued':
      return `Delegated to ${e.koraagent ?? 'an agent'}`;
    case 'identity.tenant.switched':
      return `Switched to ${str(entity.to_tenant) ?? 'another tenant'}`;
    case 'declarative.resource.failed':
      return `Declared ${str(entity.kind) ?? 'resource'} not applied${name ? `: ${name}` : ''}`;
  }
  const verb = bare.split('.').pop() ?? '';
  const noun = ENTITY_NOUN[categoryOf(bare)] ?? 'Resource';
  const label = name ?? str(entity.slug) ?? str(entity.role);
  return `${noun} ${verb}${label ? `: ${label}` : ''}`;
}

const RESOURCE_KIND: Record<string, string> = {
  runs: 'Run',
  deliveries: 'Delivery',
  triggers: 'Trigger',
  sources: 'Source',
  budgets: 'Budget',
  sessions: 'Session',
  pats: 'Access token',
  delegations: 'Delegation',
  grants: 'Grant',
  roles: 'Role',
  tenant_budgets: 'Tenant budget',
  agents: 'Agent',
  environments: 'Environment',
  declared: 'Declared resource',
};

/** What the event concerns, from its subject: `runs/2b4e8c1d-…` reads as Run 2b4e8c1d. */
export function resourceOf(e: KoraEvent): { kind: string; label: string } {
  const [collection = '', ...rest] = e.subject.split('/');
  if (collection === 'runs' && rest[1] === 'sandbox')
    return { kind: 'Sandbox', label: shortId(rest[0] ?? '') };
  const kind = RESOURCE_KIND[collection] ?? collection;
  const name = str(e.data.entity.name);
  return { kind, label: name ?? rest.map(shortId).join('/') };
}

/** The first block of a UUID. Other ids are already short. */
export function shortId(id: string): string {
  return id.length === 36 ? id.slice(0, 8) : id;
}

/** The principal's display name, or its subject when the event carries none. */
export function principalName(e: KoraEvent): string {
  return e.koraprincipalname || e.koraprincipal;
}

/** The trigger ids an event names: its chain, and the trigger behind a run or delivery. */
export function triggerIdsOf(e: KoraEvent): string[] {
  const ids = [...(e.koratriggers ?? [])];
  const own = str(e.data.entity.trigger_id);
  if (own !== undefined && !ids.includes(own)) ids.push(own);
  if (e.subject.startsWith('triggers/')) {
    const id = e.subject.slice('triggers/'.length);
    if (!ids.includes(id)) ids.push(id);
  }
  return ids;
}

/** The delivery an event concerns or came from, if any. */
export function deliveryIdOf(e: KoraEvent): string | undefined {
  if (e.subject.startsWith('deliveries/')) return e.subject.slice('deliveries/'.length);
  return str(e.data.entity.delivery_id);
}

/** The absolute time, written as the Deliveries and Runs pages write it. */
export function formatUtc(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : `${d.toISOString().slice(0, 19).replace('T', ' ')} UTC`;
}

/** The trace id inside a W3C `traceparent`. */
export function traceIdOf(traceparent: string | undefined): string | null {
  return traceparent?.split('-')[1] ?? null;
}

/** Fills the chart's trace URL template, or null when tracing has no UI configured. */
export function traceHref(template: string, traceId: string): string | null {
  return template === '' ? null : template.replaceAll('{trace_id}', traceId);
}

/** A principal or agent seen in the feed, for the column filter menus. */
export interface SeenPrincipal {
  subject: string;
  name: string;
  type: EventPrincipalType;
}
