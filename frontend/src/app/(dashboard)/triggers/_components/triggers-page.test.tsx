import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

const nav = vi.hoisted(() => ({ replace: vi.fn(), search: '' }));
vi.mock('next/navigation', () => ({
  useRouter: () => ({ refresh: vi.fn(), replace: nav.replace }),
  usePathname: () => '/triggers',
  useSearchParams: () => new URLSearchParams(nav.search),
}));
const api = vi.hoisted(() => ({ listDeliveryPage: vi.fn() }));
vi.mock('../_services/triggers-api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../_services/triggers-api')>()),
  ...api,
}));
api.listDeliveryPage.mockResolvedValue({ deliveries: [], nextCursor: null, error: null });

import { TriggersPage } from './triggers-page';
import type { SourceRow } from '../_types';
import { makeTrigger } from '../_types/fixtures';

const SOURCE: Omit<SourceRow, 'id' | 'name' | 'provider' | 'category'> = {
  enabled: true,
  signingSecretSet: true,
  createdAt: '2026-07-01T00:00:00Z',
  rejectionCount: 0,
  lastRejectionAt: null,
  lastRejectionReason: null,
  declared: false,
};
const SOURCES: SourceRow[] = [
  { ...SOURCE, id: 'gh', name: 'acme/widgets', provider: 'github', category: 'webhook' },
  { ...SOURCE, id: 'sl', name: 'acme-slack', provider: 'slack', category: 'chat' },
];
const TRIGGERS = [
  makeTrigger({ id: 'w1', name: 'on-push', sourceId: 'gh', eventType: 'push' }),
  makeTrigger({ id: 'c1', name: 'answer-mentions', sourceId: 'sl', eventType: 'mention' }),
  makeTrigger({ id: 'c2', name: 'dm-helper', sourceId: 'sl', eventType: 'direct_message' }),
];

beforeEach(() => {
  nav.search = '';
  api.listDeliveryPage.mockReset();
  api.listDeliveryPage.mockResolvedValue({ deliveries: [], nextCursor: null, error: null });
});

function renderPage() {
  return renderWithProviders(
    <TriggersPage
      sources={SOURCES}
      triggers={TRIGGERS}
      stats={null}
      range="24h"
      agents={[]}
      error={null}
    />,
  );
}

describe('the kind filter', () => {
  it('counts chat commands apart from webhooks', () => {
    renderPage();
    const filter = screen.getByRole('group', { name: 'Filter triggers by kind' });
    expect(filter.textContent).toContain('Webhook1');
    expect(filter.textContent).toContain('Chat command2');
  });

  it('lists only chat commands under Chat command', () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: /Chat command/ }));
    expect(screen.getByText('answer-mentions')).toBeTruthy();
    expect(screen.getByText('dm-helper')).toBeTruthy();
    expect(screen.queryByText('on-push')).toBeNull();
  });

  it('lists no chat command under Webhook', () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: /^Webhook/ }));
    expect(screen.getByText('on-push')).toBeTruthy();
    expect(screen.queryByText('answer-mentions')).toBeNull();
  });
});

describe('a chat source waiting for its signing secret', () => {
  it('marks its triggers and no others', () => {
    const waiting = SOURCES.map((s) => (s.id === 'sl' ? { ...s, signingSecretSet: false } : s));
    renderWithProviders(
      <TriggersPage
        sources={waiting}
        triggers={TRIGGERS}
        stats={null}
        range="24h"
        agents={[]}
        error={null}
      />,
    );
    expect(screen.getAllByText('Waiting for signing secret')).toHaveLength(2);
  });

  it('marks nothing when every source holds its secret', () => {
    renderPage();
    expect(screen.queryByText('Waiting for signing secret')).toBeNull();
  });
});

describe('a locked tenant', () => {
  it('explains the lock and offers no new trigger', () => {
    renderWithProviders(
      <TriggersPage
        sources={SOURCES}
        triggers={TRIGGERS}
        stats={null}
        range="24h"
        agents={[]}
        error={null}
      />,
      { apiAuthoring: false },
    );
    expect(screen.getByText(/Triggers and sources in this tenant are managed in git/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /New trigger/ })).toBeNull();
  });

  it('offers a new trigger when the tenant is not locked', () => {
    renderPage();
    expect(screen.queryByText(/are managed in git/)).toBeNull();
    expect(screen.getByRole('button', { name: /New trigger/ })).toBeTruthy();
  });
});

describe('the range selector', () => {
  const stats = {
    deliveries: 1234,
    started: 30,
    failed: 10,
    dispatchP50Ms: null,
    dispatchP95Ms: null,
  };
  const renderRange = (range: '7d' | 'all') =>
    renderWithProviders(
      <TriggersPage
        sources={SOURCES}
        triggers={TRIGGERS}
        stats={stats}
        range={range}
        agents={[]}
        error={null}
      />,
    );

  it('labels the stats and bounds the deliveries with the range the server loaded', () => {
    renderRange('7d');
    expect(screen.getByText('DELIVERIES · 7D')).toBeInTheDocument();
    expect(screen.getByText('1,234')).toBeInTheDocument();
    expect(api.listDeliveryPage).toHaveBeenLastCalledWith(
      expect.objectContaining({ receivedAfter: expect.any(String) }),
    );
  });

  it('shows no window counts and lists all history for All', () => {
    renderRange('all');
    expect(screen.queryByText('1,234')).toBeNull();
    expect(screen.getByText('ACTIVE TRIGGERS')).toBeInTheDocument();
    expect(api.listDeliveryPage.mock.lastCall?.[0]).not.toHaveProperty('receivedAfter');
  });

  it('navigates to the chosen range', () => {
    renderRange('7d');
    fireEvent.click(
      within(screen.getByRole('group', { name: 'Period' })).getByRole('button', { name: 'All' }),
    );
    expect(nav.replace).toHaveBeenCalledWith('/triggers?window=all', { scroll: false });
  });
});

describe('links from the Events page', () => {
  /** The API's delivery shape, as little of it as the row mapper needs. */
  const raw = (id: string) => ({
    id,
    kind: 'webhook',
    status: 'failed',
    event_type: 'push',
    received_at: '2026-07-29T04:00:00Z',
    runs: [],
  });

  it('opens the trigger named by ?trigger=', () => {
    nav.search = 'trigger=c1';
    renderPage();
    expect(screen.getByRole('dialog')).toHaveTextContent('answer-mentions');
  });

  it('opens nothing for a trigger it does not know', () => {
    nav.search = 'trigger=gone';
    renderPage();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('opens the delivery named by ?delivery=, paging back until it is found', async () => {
    nav.search = 'tab=deliveries&delivery=dlv-2';
    api.listDeliveryPage
      .mockResolvedValueOnce({ deliveries: [raw('dlv-1')], nextCursor: 'c1', error: null })
      .mockResolvedValueOnce({ deliveries: [raw('dlv-2')], nextCursor: null, error: null });
    renderPage();
    await waitFor(() => expect(screen.getByRole('dialog')).toHaveTextContent('Delivery dlv-2'));
    expect(api.listDeliveryPage).toHaveBeenCalledTimes(2);
    expect(screen.getByRole('tab', { name: 'Deliveries', hidden: true })).toHaveAttribute(
      'aria-selected',
      'true',
    );
  });

  it('stops looking for a linked delivery once a page fails', async () => {
    nav.search = 'tab=deliveries&delivery=dlv-x';
    api.listDeliveryPage
      .mockResolvedValueOnce({ deliveries: [raw('dlv-1')], nextCursor: 'c1', error: null })
      .mockResolvedValue({ deliveries: [], nextCursor: null, error: 'boom' });
    renderPage();
    await waitFor(() => expect(api.listDeliveryPage).toHaveBeenCalledTimes(2));
    await new Promise((r) => setTimeout(r, 50));
    expect(api.listDeliveryPage).toHaveBeenCalledTimes(2);
  });
});
