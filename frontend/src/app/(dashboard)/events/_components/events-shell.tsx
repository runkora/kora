'use client';

/** @tier 3 — events-scoped. Client boundary: holds the filters and runs the live feed. */

import { useEffect, useState } from 'react';
import { NO_FILTERS } from '../_types';
import type { EventFilters } from '../_types';
import { useEventFeed } from '../_hooks/use-event-feed';
import type { InitialEvents } from '../_hooks/use-event-feed';
import { EventsPage } from './events-page';

/** How long the search box waits for typing to stop before it asks the server. */
const SEARCH_DEBOUNCE_MS = 300;

interface EventsShellProps {
  initial: InitialEvents;
  traceUrlTemplate: string;
}

export function EventsShell({ initial, traceUrlTemplate }: EventsShellProps) {
  const [filters, setFilters] = useState<EventFilters>(NO_FILTERS);
  const [query, setQuery] = useState('');
  useEffect(() => {
    const timer = setTimeout(() => setQuery(filters.query), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [filters.query]);
  // Clearing the search resumes live at once, without the debounce.
  const settled = filters.query === '' ? '' : query;
  const feed = useEventFeed({ ...filters, query: settled }, initial);

  return (
    <EventsPage
      feed={feed}
      filters={filters}
      onFiltersChange={setFilters}
      traceUrlTemplate={traceUrlTemplate}
    />
  );
}
