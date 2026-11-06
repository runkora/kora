import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@runkora/test-utils';

vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn() }) }));
const api = vi.hoisted(() => ({
  listDeliveryPage: vi.fn(),
  listTriggers: vi.fn(),
}));
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

function renderPage() {
  return renderWithProviders(
    <TriggersPage sources={SOURCES} triggers={TRIGGERS} stats={null} agents={[]} error={null} />,
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
      <TriggersPage sources={waiting} triggers={TRIGGERS} stats={null} agents={[]} error={null} />,
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
      <TriggersPage sources={SOURCES} triggers={TRIGGERS} stats={null} agents={[]} error={null} />,
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
  it('reads the 7d stats for the cards and the list, and bounds the deliveries to 7d', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2026-11-06T12:00:00Z'));
    const bucket = { start: '2026-11-06T00:00:00Z', started: 2, failed: 1, other: 40 };
    api.listTriggers.mockResolvedValue({
      triggers: [
        {
          id: 'w1',
          stats: {
            last_fired_at: null,
            deliveries: 43,
            started: 2,
            failed: 1,
            status_counts: { filtered: 40 },
            buckets: [bucket],
          },
        },
      ],
      stats: {
        deliveries: 1234,
        started: 30,
        failed: 10,
        dispatch_p50_ms: null,
        dispatch_p95_ms: null,
      },
      error: null,
    });
    renderWithProviders(
      <TriggersPage
        sources={SOURCES}
        triggers={TRIGGERS}
        stats={{ deliveries: 5, started: 5, failed: 0, dispatchP50Ms: null, dispatchP95Ms: null }}
        agents={[]}
        error={null}
      />,
    );
    expect(screen.getByText('DELIVERIES · 24H')).toBeInTheDocument();
    expect(api.listTriggers).not.toHaveBeenCalled();

    fireEvent.click(
      within(screen.getByRole('group', { name: 'Range' })).getByRole('button', { name: '7d' }),
    );
    await waitFor(() => expect(screen.getByText('1,234')).toBeInTheDocument());
    expect(api.listTriggers).toHaveBeenCalledWith({ window: '7d' });
    expect(screen.getByText('DELIVERIES · 7D')).toBeInTheDocument();
    expect(
      screen.getByRole('img', { name: 'on-push: runs started and failed over the last 7d' }),
    ).toBeInTheDocument();
    expect(api.listDeliveryPage).toHaveBeenLastCalledWith(
      expect.objectContaining({ receivedAfter: '2026-10-30T12:00:00.000Z' }),
    );
    now.mockRestore();
  });
});

describe('the Deliveries tab pager', () => {
  const raw = (i: number) => ({
    id: `d-${i}`,
    kind: 'webhook',
    status: 'skipped',
    received_at: '2026-11-06T10:00:00Z',
    runs: [],
  });

  it('fetches the next server page when the next page runs past the loaded rows', async () => {
    api.listDeliveryPage
      .mockResolvedValueOnce({
        deliveries: Array.from({ length: 12 }, (_, i) => raw(i)),
        nextCursor: 'c1',
        error: null,
      })
      .mockResolvedValueOnce({
        deliveries: [raw(12), raw(13), raw(14)],
        nextCursor: null,
        error: null,
      });
    renderPage();
    await userEvent.click(screen.getByRole('tab', { name: 'Deliveries' }));
    await waitFor(() => expect(screen.getByText(/1–12 of 12\+/)).toBeInTheDocument());
    expect(screen.queryByRole('button', { name: 'Load older' })).toBeNull();

    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(screen.getByText(/13–15 of 15$/)).toBeInTheDocument());
    expect(api.listDeliveryPage).toHaveBeenLastCalledWith(
      expect.objectContaining({ cursor: 'c1' }),
    );
    expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled();
  });
});
