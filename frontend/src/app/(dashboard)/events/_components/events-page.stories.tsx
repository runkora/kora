'use client';

import { useMemo, useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage } from '@runkora/storybook/dashboard-frame';
import { NO_FILTERS, categoryOf, principalName, tabCategories } from '../_types';
import type { EventFilters, KoraEvent, SeenPrincipal } from '../_types';
import type { EventFeed, StreamState } from '../_hooks/use-event-feed';
import { EventsPage } from './events-page';
import { TRACE_URL_TEMPLATE, buildEvents } from './events-story-data';

/** Which fixture event a detail story opens. */
type OpenEvent = 'changed-fields' | 'run-failed' | 'chained';

const PICK: Record<OpenEvent, (e: KoraEvent) => boolean> = {
  'changed-fields': (e) => e.type === 'policy.grant.updated.v1',
  'run-failed': (e) => e.type === 'agents.run.failed.v1',
  chained: (e) => e.type === 'agents.run.created.v1' && e.korachaindepth > 0,
};

interface StoryArgs {
  feed?: Partial<Pick<EventFeed, 'loading' | 'error' | 'loadingMore' | 'pending' | 'hasMore'>> & {
    stream?: StreamState;
  };
  /** Start with no history, as a new tenant does. */
  noHistory?: boolean;
  filters?: EventFilters;
  open?: OpenEvent;
}

/** Stands in for the server: the filters the API applies, over the fixture. */
function matches(e: KoraEvent, f: EventFilters): boolean {
  const categories = tabCategories(f.tab);
  const q = f.query.trim().toLowerCase();
  return (
    (categories.length === 0 || categories.includes(categoryOf(e.type))) &&
    (f.types.length === 0 || f.types.includes(e.type)) &&
    (f.agents.length === 0 || f.agents.includes(e.koraagent ?? '')) &&
    (f.principals.length === 0 || f.principals.includes(e.koraprincipal)) &&
    (q === '' ||
      [e.type, e.subject, principalName(e), e.koraagent ?? '', e.korarun ?? ''].some((v) =>
        v.toLowerCase().includes(q),
      ))
  );
}

/** The page over a fixed feed. The filters work, and the stream holds still for screenshots. */
function EventsStory({
  feed = {},
  noHistory = false,
  filters: start = NO_FILTERS,
  open,
}: StoryArgs) {
  const history = useMemo(() => (noHistory ? [] : buildEvents()), [noHistory]);
  const [filters, setFilters] = useState(start);
  const principals = useMemo<SeenPrincipal[]>(
    () =>
      [...new Map(history.map((e) => [e.koraprincipal, e])).values()].map((e) => ({
        subject: e.koraprincipal,
        name: principalName(e),
        type: e.koraprincipaltype,
      })),
    [history],
  );
  const agents = useMemo(
    () => [...new Set(history.flatMap((e) => (e.koraagent ? [e.koraagent] : [])))].sort(),
    [history],
  );
  const shown = feed.loading || feed.error ? [] : history.filter((e) => matches(e, filters));
  const noop = () => undefined;
  const value: EventFeed = {
    events: shown,
    loading: feed.loading ?? false,
    error: feed.error ?? null,
    olderError: null,
    retry: noop,
    hasMore: feed.hasMore ?? shown.length > 0,
    loadingMore: feed.loadingMore ?? false,
    loadOlder: noop,
    stream: filters.query.trim() !== '' ? 'search' : (feed.stream ?? 'live'),
    setPaused: noop,
    pending: feed.pending ?? 0,
    showPending: noop,
    freshIds: new Set(history.slice(0, 1).map((e) => e.id)),
    principals,
    agents,
  };
  const openId = open === undefined ? null : (history.find(PICK[open])?.id ?? null);
  return (
    <EventsPage
      feed={value}
      filters={filters}
      onFiltersChange={setFilters}
      traceUrlTemplate={TRACE_URL_TEMPLATE}
      initialOpenId={openId}
    />
  );
}

/** The Events page: every platform event in a table, with live updates and a detail modal. */
const meta: Meta<typeof EventsStory> = {
  title: 'Pages/Events',
  component: EventsStory,
  decorators: [dashboardPage()],
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/events' } },
  },
};

export default meta;
type Story = StoryObj<typeof EventsStory>;

/** A busy tenant, live. The newest event is marked as new. */
export const Default: Story = {};

/** An admin edit open: the grant's changed fields and its state after the change. */
export const DetailChangedFields: Story = { args: { open: 'changed-fields' } };

/** A failed run open: its trigger, run and trace links, and the event trigger it set off. */
export const DetailRunFailed: Story = { args: { open: 'run-failed' } };

/** A run an event trigger started: the cause it links back to and its chain depth. */
export const DetailChained: Story = { args: { open: 'chained' } };

/** Runs of one agent only. */
export const Filtered: Story = {
  args: { filters: { ...NO_FILTERS, tab: 'runs', agents: ['triage'] } },
};

/** Header filters on two columns at once: Priya's events and the pr-reviewer agent's. */
export const ColumnFilters: Story = {
  args: { filters: { ...NO_FILTERS, principals: ['usr_8c1f20'], agents: ['pr-reviewer'] } },
};

/** The Principal header filter open: a searchable, multi-select list with one principal picked. */
export const PrincipalFilterOpen: Story = {
  args: { filters: { ...NO_FILTERS, principals: ['usr_8c1f20'] } },
  play: async ({ canvasElement }) => {
    const buttons = await within(canvasElement).findAllByRole('button', { name: 'Filter' });
    const principal = buttons[1];
    if (principal === undefined) throw new Error('no principal filter button');
    await userEvent.click(principal);
  },
};

/** A search pauses the feed on matching history until it is cleared. */
export const Search: Story = { args: { filters: { ...NO_FILTERS, query: 'triage' } } };

/** A search that matches nothing. The header stays so the search can be changed. */
export const SearchNoResults: Story = {
  args: { filters: { ...NO_FILTERS, query: 'payments-reconciler' } },
};

/** Paused, with three events held back behind the new-events button. */
export const NewEventsWaiting: Story = { args: { feed: { stream: 'paused', pending: 3 } } };

/** "Load older" fetching the next page back. */
export const LoadingOlder: Story = { args: { feed: { loadingMore: true } } };

/** Every event loaded: the footer says the history starts here. */
export const HistoryEnd: Story = { args: { feed: { hasMore: false } } };

/** The live stream dropped. History still shows. */
export const Reconnecting: Story = { args: { feed: { stream: 'reconnecting' } } };

/** The first page has not landed. */
export const Loading: Story = { args: { feed: { loading: true } } };

/** The first page failed. */
export const LoadError: Story = {
  args: { feed: { error: 'The events service did not answer (503). Try again in a moment.' } },
};

/** A new tenant with no events yet. */
export const Empty: Story = { args: { noHistory: true } };

/** Filters that match nothing. */
export const FilteredEmpty: Story = {
  args: { filters: { ...NO_FILTERS, tab: 'declared', agents: ['triage'] } },
};
