'use client';

/**
 * @tier 3 — events-scoped. The events table: one row per event as the Deliveries tab draws its
 * feed, with the category tabs, Columns and server search in its toolbar and searchable
 * multi-select filters on the Event, Principal and Agent headers. A row opens the detail modal.
 */

import { useMemo } from 'react';
import { DataTable, Icon, Stack, StatusDot, Text } from '@runkora/hades';
import type { DataTableColumn, DataTableFilterOption, DataTableServerFilter } from '@runkora/hades';
import { relativeTime } from '@runkora/lib/relative-time';
import {
  categoryOf,
  eventSummary,
  eventTone,
  formatUtc,
  principalName,
  resourceOf,
  tabCategories,
} from '../_types';
import type { EventFilters, KoraEvent, SeenPrincipal } from '../_types';
import type { EventFeed } from '../_hooks/use-event-feed';
import { CategoryTabs, EventPrincipalBadge, FeedEmpty, FeedLoading, NewBadge } from './event-parts';

interface Row {
  e: KoraEvent;
  /** The loaded event that caused this one, for the chain line. */
  cause?: KoraEvent;
  delegator?: string;
  fresh: boolean;
}

type ListKey = 'types' | 'agents' | 'principals';

/** A server-mode header filter over one of the filter lists. */
function headerFilter(
  filters: EventFilters,
  onChange: (next: EventFilters) => void,
  key: ListKey,
  options: DataTableFilterOption[],
  mono = false,
): DataTableServerFilter {
  const selected = filters[key];
  // A selected value the feed has not shown yet still needs its option.
  const missing = selected
    .filter((v) => !options.some((o) => o.value === v))
    .map((v) => ({ value: v, label: v }));
  return {
    mode: 'server',
    searchable: true,
    mono,
    options: [...options, ...missing],
    selected: new Set(selected),
    onToggle: (value) =>
      onChange({
        ...filters,
        [key]: selected.includes(value)
          ? selected.filter((v) => v !== value)
          : [...selected, value],
      }),
    onClear: () => onChange({ ...filters, [key]: [] }),
  };
}

function columns(
  filters: EventFilters,
  onChange: (next: EventFilters) => void,
  principals: SeenPrincipal[],
  agents: string[],
  catalog: string[],
): DataTableColumn<Row>[] {
  const categories = tabCategories(filters.tab);
  const types = catalog.filter(
    (t) => categories.length === 0 || categories.includes(categoryOf(t)),
  );
  return [
    {
      id: 'time',
      header: 'Time',
      gridWidth: '96px',
      minWidth: '5.5rem',
      cell: ({ e }) => (
        <Text size="supporting" color="muted" title={formatUtc(e.time)}>
          {relativeTime(e.time)}
        </Text>
      ),
    },
    {
      id: 'event',
      header: 'Event',
      gridWidth: 'minmax(0, 1.7fr)',
      minWidth: '15rem',
      locked: true,
      filter: headerFilter(
        filters,
        onChange,
        'types',
        types.map((t) => ({ value: t, label: t })),
        true,
      ),
      cell: ({ e, cause, fresh }) => (
        <Stack direction="horizontal" align="center" gap="cozy" minWidth="none">
          <StatusDot status={eventTone(e.type)} size="sm" />
          <Stack gap="tight" minWidth="none" grow>
            <Text size="supporting" weight="emphasis" truncate>
              {eventSummary(e)}
            </Text>
            <Text size="caption" mono color="subtle" truncate>
              {e.type}
            </Text>
            {e.korachaindepth > 0 && (
              <Stack direction="horizontal" align="center" gap="snug" minWidth="none">
                <Icon name="git-branch" size="2xs" color="muted" label="" shrink />
                <Text size="caption" color="subtle" truncate>
                  Chain depth {e.korachaindepth}, caused by{' '}
                  {cause === undefined ? 'an older event' : eventSummary(cause)}
                </Text>
              </Stack>
            )}
          </Stack>
          {fresh && <NewBadge />}
        </Stack>
      ),
    },
    {
      id: 'principal',
      header: 'Principal',
      gridWidth: 'minmax(0, 1fr)',
      minWidth: '9rem',
      filter: headerFilter(
        filters,
        onChange,
        'principals',
        principals.map((p) => ({ value: p.subject, label: p.name })),
      ),
      cell: ({ e, delegator }) => (
        <Stack gap="tight" minWidth="none">
          <Text size="supporting" truncate title={e.koraprincipal}>
            {principalName(e)}
          </Text>
          {delegator !== undefined && (
            <Text size="caption" color="subtle" truncate>
              for {delegator}
            </Text>
          )}
        </Stack>
      ),
    },
    {
      id: 'principalType',
      header: 'Principal type',
      gridWidth: '104px',
      minWidth: '5.5rem',
      cell: ({ e }) => (
        <Stack direction="horizontal">
          <EventPrincipalBadge type={e.koraprincipaltype} />
        </Stack>
      ),
    },
    {
      id: 'resource',
      header: 'Resource',
      gridWidth: 'minmax(0, 1fr)',
      minWidth: '9rem',
      cell: ({ e }) => {
        const r = resourceOf(e);
        return (
          <Stack gap="tight" minWidth="none">
            <Text size="supporting" mono truncate title={e.subject}>
              {r.label}
            </Text>
            <Text size="caption" color="subtle" truncate>
              {r.kind}
            </Text>
          </Stack>
        );
      },
    },
    {
      id: 'agent',
      header: 'Agent',
      gridWidth: '160px',
      minWidth: '7rem',
      filter: headerFilter(
        filters,
        onChange,
        'agents',
        agents.map((a) => ({ value: a, label: a })),
        true,
      ),
      cell: ({ e }) => (
        <Text size="supporting" mono color="muted" truncate>
          {e.koraagent ?? ''}
        </Text>
      ),
    },
  ];
}

export interface EventsTableProps {
  feed: EventFeed;
  filters: EventFilters;
  onFiltersChange: (next: EventFilters) => void;
  eventTypes: string[];
  filtered: boolean;
  onClearFilters: () => void;
  onOpen: (id: string) => void;
}

export function EventsTable({
  feed,
  filters,
  onFiltersChange,
  eventTypes,
  filtered,
  onClearFilters,
  onOpen,
}: EventsTableProps) {
  const rows = useMemo<Row[]>(() => {
    const byId = new Map(feed.events.map((e) => [e.id, e]));
    const names = new Map(feed.principals.map((p) => [p.subject, p.name]));
    return feed.events.map((e) => ({
      e,
      ...(e.koracausedby !== undefined && { cause: byId.get(e.koracausedby) }),
      ...(e.koradelegator !== undefined && {
        delegator: names.get(e.koradelegator) ?? e.koradelegator,
      }),
      fresh: feed.freshIds.has(e.id),
    }));
  }, [feed.events, feed.freshIds, feed.principals]);
  // The platform's types, and any the feed has shown that the catalog did not list.
  const catalog = useMemo(
    () => [...new Set([...eventTypes, ...feed.events.map((e) => e.type)])].sort(),
    [eventTypes, feed.events],
  );
  const cols = useMemo(
    () => columns(filters, onFiltersChange, feed.principals, feed.agents, catalog),
    [filters, onFiltersChange, feed.principals, feed.agents, catalog],
  );

  // Loading, failure and no rows render inside the table body, so the header and its filters
  // stay in place and a filter that matches nothing can be changed back.
  const bodyState = feed.loading ? (
    <FeedLoading />
  ) : rows.length === 0 ? (
    <FeedEmpty feed={feed} filtered={filtered} onClearFilters={onClearFilters} />
  ) : undefined;

  return (
    <DataTable
      data={rows}
      columns={cols}
      getRowKey={({ e }) => e.id}
      noun="events"
      quickFilters={<CategoryTabs filters={filters} onChange={onFiltersChange} />}
      searchQuery={filters.query}
      onSearchChange={(query) => onFiltersChange({ ...filters, query })}
      searchPlaceholder="Search events…"
      bodyState={bodyState}
      onRowClick={({ e }) => onOpen(e.id)}
      rowLabel={({ e }) => `Open event ${eventSummary(e)}`}
    />
  );
}
