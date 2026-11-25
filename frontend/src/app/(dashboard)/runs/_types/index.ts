// The Runs page's filters and how they become the run list's query.

import type { RunState } from '@runkora/shared/api';

/** Runs per page; kora-agents serves at most 200. */
export const RUNS_PAGE_SIZE = 50;

export type QuickFilter = 'all' | 'waiting' | 'mine' | 'active' | 'failed';

/** The quick filters in chip order, with the run states each keeps. Mine filters by starter. */
export const QUICK_FILTERS: { value: QuickFilter; label: string; states?: RunState[] }[] = [
  { value: 'all', label: 'All' },
  { value: 'waiting', label: 'Waiting', states: ['idle'] },
  { value: 'mine', label: 'Mine' },
  { value: 'active', label: 'Active', states: ['pending', 'starting', 'active', 'completing'] },
  { value: 'failed', label: 'Failed', states: ['failed', 'budget_exhausted'] },
];

/** Run sources as `trigger_kind` values; `none` is a run no trigger started. */
export const RUN_SOURCES = [
  { value: 'none', label: 'people' },
  { value: 'webhook', label: 'webhooks' },
  { value: 'scheduled', label: 'schedules' },
  { value: 'event', label: 'events' },
] as const;

export type RunSourceFilter = (typeof RUN_SOURCES)[number]['value'];

export interface RunFilters {
  quick: QuickFilter;
  /** An agent type id, or null for every agent. */
  agentTypeId: string | null;
  source: RunSourceFilter | null;
  /** Search text, matched on the server against title, agent and starter. */
  query: string;
}

export const DEFAULT_RUN_FILTERS: RunFilters = {
  quick: 'all',
  agentTypeId: null,
  source: null,
  query: '',
};

/** The run list and stream query for the filters, without page params. */
export function runFilterParams(filters: RunFilters): URLSearchParams {
  const params = new URLSearchParams();
  const states = QUICK_FILTERS.find((f) => f.value === filters.quick)?.states;
  if (states) params.set('state', states.join(','));
  if (filters.quick === 'mine') params.set('created_by', 'me');
  if (filters.agentTypeId) params.set('agent_type_id', filters.agentTypeId);
  if (filters.source) params.set('trigger_kind', filters.source);
  const q = filters.query.trim();
  if (q) params.set('q', q);
  return params;
}
