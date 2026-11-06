'use client';

/**
 * @tier 3 — events-scoped. The pieces around the events table: the category tabs, the live
 * status with its pause control, the "N new events" button, the history footer, the principal
 * badge and the loading and empty states.
 */

import {
  Badge,
  Button,
  EmptyState,
  Icon,
  SegmentedControl,
  Skeleton,
  Stack,
  StatusDot,
  Text,
} from '@runkora/hades';
import { PrincipalBadge } from '@runkora/components';
import { TABS, categoryOf, tabCategories } from '../_types';
import type { EventFilters, EventPrincipalType, EventTab, KoraEvent } from '../_types';
import type { EventFeed, StreamState } from '../_hooks/use-event-feed';

/** The wire principal types drawn with the shared badge. The platform itself reads System. */
export function EventPrincipalBadge({ type }: { type: EventPrincipalType }) {
  if (type === 'SYSTEM')
    return (
      <Badge variant="secondary" size="sm">
        System
      </Badge>
    );
  return (
    <PrincipalBadge type={type === 'USER' ? 'HUMAN' : type === 'TRIGGER' ? 'SERVICE' : type} />
  );
}

export function NewBadge() {
  return (
    <Badge variant="default" size="sm">
      New
    </Badge>
  );
}

type FilterProps = { filters: EventFilters; onChange: (next: EventFilters) => void };

/** The category tabs. A tab drops any type filter from another category. */
export function CategoryTabs({ filters, onChange }: FilterProps) {
  return (
    <SegmentedControl
      aria-label="Filter events by category"
      value={filters.tab}
      onChange={(v) => {
        const tab = v as EventTab;
        const categories = tabCategories(tab);
        onChange({
          ...filters,
          tab,
          types:
            tab === 'all'
              ? filters.types
              : filters.types.filter((t) => categories.includes(categoryOf(t))),
        });
      }}
      options={TABS.map(({ value, label }) => ({ value, label }))}
    />
  );
}

const STREAM_COPY: Record<StreamState, { label: string; hint: string }> = {
  live: { label: 'Live', hint: 'New events appear as they happen.' },
  paused: { label: 'Paused', hint: 'New events wait until you resume.' },
  reconnecting: { label: 'Reconnecting', hint: 'The live stream dropped. Retrying.' },
  search: {
    label: 'Paused (search active)',
    hint: 'Showing matching history only. Clear the search to resume live updates.',
  },
};

/** Whether the feed is live, with the control that pauses it. */
export function LiveControl({ feed }: { feed: Pick<EventFeed, 'stream' | 'setPaused'> }) {
  const copy = STREAM_COPY[feed.stream];
  return (
    <Stack direction="horizontal" align="center" gap="cozy" shrink={false}>
      <Stack direction="horizontal" align="center" gap="snug" title={copy.hint}>
        <StatusDot
          size="sm"
          status={
            feed.stream === 'live'
              ? 'healthy'
              : feed.stream === 'reconnecting'
                ? 'degraded'
                : 'muted'
          }
          pulse={feed.stream === 'live' || feed.stream === 'reconnecting'}
          glow={feed.stream === 'live'}
        />
        <Text as="span" size="supporting" weight="emphasis">
          {copy.label}
        </Text>
      </Stack>
      {(feed.stream === 'live' || feed.stream === 'paused') && (
        <Button
          size="xs"
          variant="secondary"
          onClick={() => feed.setPaused(feed.stream === 'live')}
        >
          <Icon name={feed.stream === 'live' ? 'minus' : 'play'} size="2xs" />
          {feed.stream === 'live' ? 'Pause' : 'Resume'}
        </Button>
      )}
    </Stack>
  );
}

/** Shown beside the live control while events are held back. */
export function NewEventsButton({ feed }: { feed: Pick<EventFeed, 'pending' | 'showPending'> }) {
  if (feed.pending === 0) return null;
  return (
    <Button size="xs" variant="accent-outline" shape="pill" onClick={feed.showPending}>
      <Icon name="arrow-up" size="2xs" />
      {feed.pending} new {feed.pending === 1 ? 'event' : 'events'}
    </Button>
  );
}

/** Under the table: load the next page back, or say the history ends here. */
export function HistoryFooter({
  feed,
}: {
  feed: Pick<EventFeed, 'hasMore' | 'loadingMore' | 'loadOlder' | 'events' | 'error'>;
}) {
  if (feed.events.length === 0) return null;
  if (feed.hasMore)
    return (
      <Stack gap="snug" align="center">
        {feed.error !== null && (
          <Text size="caption" color="status-error">
            Could not load older events: {feed.error}
          </Text>
        )}
        <Button size="xs" variant="secondary" disabled={feed.loadingMore} onClick={feed.loadOlder}>
          <Icon name={feed.loadingMore ? 'loader' : 'arrow-down'} size="2xs" />
          {feed.loadingMore ? 'Loading older events' : 'Load older events'}
        </Button>
      </Stack>
    );
  return (
    <Text size="caption" color="subtle" align="center">
      That is the start of the history.
    </Text>
  );
}

/** The first page has not landed. */
export function FeedLoading({ rows = 6 }: { rows?: number }) {
  return (
    <Stack gap="cozy" role="status" aria-label="Loading events" aria-busy="true">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} shape="block" width="full" size="lg" />
      ))}
    </Stack>
  );
}

/** No rows: an error, a search or filter that matches nothing, or a tenant with no events yet. */
export function FeedEmpty({
  feed,
  filtered,
  onClearFilters,
}: {
  feed: Pick<EventFeed, 'error' | 'retry' | 'stream'>;
  filtered: boolean;
  onClearFilters: () => void;
}) {
  if (feed.error !== null)
    return (
      <EmptyState
        icon="triangle-alert"
        title="Could not load events"
        description={feed.error}
        action={
          <Button size="xs" variant="secondary" onClick={feed.retry}>
            <Icon name="refresh-cw" size="2xs" />
            Try again
          </Button>
        }
      />
    );
  if (feed.stream === 'search')
    return (
      <EmptyState
        icon="search-x"
        title="No events match this search"
        description="The feed is paused while you search. Clear the search to resume live updates."
        action={
          <Button size="xs" variant="secondary" onClick={onClearFilters}>
            Clear search and filters
          </Button>
        }
      />
    );
  if (filtered)
    return (
      <EmptyState
        icon="search-x"
        title="No events match these filters"
        description="New events that match will appear here while the feed is live."
        action={
          <Button size="xs" variant="secondary" onClick={onClearFilters}>
            Clear filters
          </Button>
        }
      />
    );
  return (
    <EmptyState
      icon="activity"
      title="No events yet"
      description="Runs, trigger deliveries, sign-ins and configuration changes appear here the moment they happen."
    />
  );
}

/** The loaded events this event caused. */
export function effectsOf(event: KoraEvent, all: KoraEvent[]): KoraEvent[] {
  return all.filter((e) => e.koracausedby === event.id);
}

/** The event with this id among those loaded. */
export function findEvent(
  events: KoraEvent[],
  id: string | null | undefined,
): KoraEvent | undefined {
  return id == null ? undefined : events.find((e) => e.id === id);
}
