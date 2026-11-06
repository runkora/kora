'use client';

/**
 * @tier 3 — events-scoped. The Events page: the heading, the live control above the table, the
 * stream's state, the table and its history footer, and the open event's modal.
 */

import { useState } from 'react';
import { Callout, Heading, Stack, Text } from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import { NO_FILTERS, isFiltered } from '../_types';
import type { EventFilters } from '../_types';
import type { EventFeed } from '../_hooks/use-event-feed';
import { HistoryFooter, LiveControl, NewEventsButton, findEvent } from './event-parts';
import { EventDetailModal } from './event-detail-modal';
import { EventsTable } from './events-table';

export interface EventsPageProps {
  feed: EventFeed;
  filters: EventFilters;
  onFiltersChange: (next: EventFilters) => void;
  /** The chart's trace URL template; empty when tracing has no UI. */
  traceUrlTemplate: string;
  /** The event whose detail opens with the page, for stories. */
  initialOpenId?: string | null;
}

export function EventsPage({
  feed,
  filters,
  onFiltersChange,
  traceUrlTemplate,
  initialOpenId = null,
}: EventsPageProps) {
  const { productName } = useBrand();
  const [openId, setOpenId] = useState<string | null>(initialOpenId);
  const open = findEvent(feed.events, openId);

  return (
    <Stack gap="comfortable">
      <Stack gap="tight" maxWidth="prose">
        <Heading variant="section">Events</Heading>
        <Text size="supporting" color="muted">
          Everything that happens on {productName}, as it happens: runs and their sandboxes, trigger
          deliveries, budgets, sign-ins and tokens, and every change to agents, policy and triggers.
        </Text>
      </Stack>

      {feed.stream === 'reconnecting' && (
        <Callout variant="warning">
          <Text size="caption">
            The live stream dropped and is reconnecting. Events that happen meanwhile appear once it
            is back.
          </Text>
        </Callout>
      )}

      <Stack gap="cozy">
        <Stack direction="horizontal" align="center" justify="end" gap="cozy">
          <NewEventsButton feed={feed} />
          <LiveControl feed={feed} />
        </Stack>
        <EventsTable
          feed={feed}
          filters={filters}
          onFiltersChange={onFiltersChange}
          filtered={isFiltered(filters)}
          onClearFilters={() => onFiltersChange(NO_FILTERS)}
          onOpen={setOpenId}
        />
        <HistoryFooter feed={feed} />
      </Stack>

      {open !== undefined && (
        <EventDetailModal
          event={open}
          events={feed.events}
          principals={feed.principals}
          traceUrlTemplate={traceUrlTemplate}
          onOpen={setOpenId}
          onClose={() => setOpenId(null)}
        />
      )}
    </Stack>
  );
}
