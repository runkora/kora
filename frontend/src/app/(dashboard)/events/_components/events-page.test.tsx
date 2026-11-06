import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, within } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { NO_FILTERS, eventSummary, principalName } from '../_types';
import type { EventFilters } from '../_types';
import type { EventFeed } from '../_hooks/use-event-feed';
import { EventsPage } from './events-page';
import { TRACE_URL_TEMPLATE, buildEvents } from './events-story-data';

const EVENTS = buildEvents(Date.parse('2026-11-06T08:00:00Z'));

function feed(over: Partial<EventFeed> = {}): EventFeed {
  return {
    events: EVENTS,
    loading: false,
    error: null,
    olderError: null,
    retry: vi.fn(),
    hasMore: true,
    loadingMore: false,
    loadOlder: vi.fn(),
    stream: 'live',
    setPaused: vi.fn(),
    pending: 0,
    showPending: vi.fn(),
    freshIds: new Set(),
    principals: EVENTS.map((e) => ({
      subject: e.koraprincipal,
      name: principalName(e),
      type: e.koraprincipaltype,
    })),
    agents: ['triage'],
    types: [],
    ...over,
  };
}

function renderPage(
  f: EventFeed,
  filters: EventFilters = NO_FILTERS,
  onChange = vi.fn(),
  eventTypes = [...new Set(EVENTS.map((e) => e.type))],
) {
  renderWithProviders(
    <EventsPage
      feed={f}
      filters={filters}
      onFiltersChange={onChange}
      eventTypes={eventTypes}
      traceUrlTemplate={TRACE_URL_TEMPLATE}
    />,
  );
  return onChange;
}

describe('EventsPage', () => {
  it('offers the nine resource tabs after All, without counts', () => {
    renderPage(feed());
    const tabs = screen.getByRole('group', { name: 'Filter events by category' });
    expect(
      within(tabs)
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual([
      'All',
      'Runs',
      'Sandboxes',
      'Agents',
      'Environments',
      'Triggers',
      'Budgets',
      'Identity',
      'Policy',
      'Declared',
    ]);
  });

  it('passes the search text up for the server', () => {
    const onChange = renderPage(feed());
    fireEvent.change(screen.getByPlaceholderText('Search events…'), {
      target: { value: 'triage' },
    });
    expect(onChange).toHaveBeenCalledWith({ ...NO_FILTERS, query: 'triage' });
  });

  it('says a search pauses the feed, with no pause button', () => {
    renderPage(feed({ stream: 'search' }), { ...NO_FILTERS, query: 'triage' });
    expect(screen.getByText('Paused (search active)')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Pause/ })).toBeNull();
  });

  it('shows the held-back count while paused', () => {
    renderPage(feed({ stream: 'paused', pending: 3 }));
    expect(screen.getByRole('button', { name: /3 new events/ })).toBeInTheDocument();
  });

  it('keeps the header and offers a way back when filters match nothing', () => {
    const onChange = renderPage(feed({ events: [] }), { ...NO_FILTERS, tab: 'declared' });
    expect(screen.getByText('No events match these filters')).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: /Principal$/ })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(onChange).toHaveBeenCalledWith(NO_FILTERS);
  });

  it('opens a failed delivery with links to its trigger and delivery on the Triggers page', () => {
    renderPage(feed());
    fireEvent.click(
      screen.getByRole('row', { name: /Open event Delivery could not start its run/ }),
    );
    const dialog = screen.getByRole('dialog');
    expect(within(dialog).getByRole('link', { name: 'sentry-new-issue-triage' })).toHaveAttribute(
      'href',
      '/triggers?trigger=trg_01JC7Q2M',
    );
    expect(within(dialog).getByRole('link', { name: 'dlv_920ff1e7' })).toHaveAttribute(
      'href',
      '/triggers?tab=deliveries&delivery=dlv_920ff1e7',
    );
  });

  it('steps to the next older event without closing', () => {
    renderPage(feed());
    fireEvent.click(screen.getAllByRole('row', { name: /^Open event/ })[0] as HTMLElement);
    expect(screen.getByRole('heading', { name: eventSummary(EVENTS[0]!) })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Older event' }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: eventSummary(EVENTS[1]!) })).toBeInTheDocument();
  });

  it('offers the types the feed has shown until the catalog arrives', () => {
    renderPage(feed({ types: ['agents.run.failed.v1'] }), NO_FILTERS, vi.fn(), []);
    fireEvent.click(screen.getAllByRole('button', { name: 'Filter' })[0] as HTMLElement);
    const options = screen.getByLabelText('Filter options');
    expect(
      within(options)
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual(['agents.run.failed.v1']);
  });

  it('adds the catalog to the types the feed has shown', () => {
    renderPage(feed({ types: ['agents.run.failed.v1'] }), NO_FILTERS, vi.fn(), [
      'policy.grant.created.v1',
    ]);
    fireEvent.click(screen.getAllByRole('button', { name: 'Filter' })[0] as HTMLElement);
    const options = screen.getByLabelText('Filter options');
    expect(
      within(options)
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual(['agents.run.failed.v1', 'policy.grant.created.v1']);
  });
});
