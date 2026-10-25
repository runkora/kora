import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

const { refresh } = vi.hoisted(() => ({ refresh: vi.fn() }));
vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh }) }));

import { TriggersPage } from './triggers-page';
import type { SourceRow } from '../_types';
import { makeTrigger } from '../_types/fixtures';

const SOURCE: Omit<SourceRow, 'id' | 'name' | 'provider' | 'category'> = {
  enabled: true,
  signingSecretSet: true,
  connection: null,
  createdAt: '2026-07-01T00:00:00Z',
  rejectionCount: 0,
  lastRejectionAt: null,
  lastRejectionReason: null,
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
    <TriggersPage sources={SOURCES} triggers={TRIGGERS} deliveries={[]} agents={[]} error={null} />,
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
        deliveries={[]}
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

describe('a Discord source that is connecting', () => {
  const discord = (state: 'connecting' | 'connected'): SourceRow[] => [
    {
      ...SOURCE,
      id: 'dc',
      name: 'acme-discord',
      provider: 'discord',
      category: 'chat',
      connection: { state },
    },
  ];
  const page = (sources: SourceRow[]) => (
    <TriggersPage sources={sources} triggers={[]} deliveries={[]} agents={[]} error={null} />
  );

  afterEach(() => {
    vi.useRealTimers();
    refresh.mockClear();
  });

  it('refreshes every 10 seconds until it is no longer connecting', () => {
    vi.useFakeTimers();
    const view = renderWithProviders(page(discord('connecting')));
    expect(refresh).not.toHaveBeenCalled();
    vi.advanceTimersByTime(10_000);
    expect(refresh).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(20_000);
    expect(refresh).toHaveBeenCalledTimes(3);

    view.rerender(page(discord('connected')));
    vi.advanceTimersByTime(60_000);
    expect(refresh).toHaveBeenCalledTimes(3);
  });

  it('does not refresh when no source is connecting', () => {
    vi.useFakeTimers();
    renderWithProviders(page(discord('connected')));
    vi.advanceTimersByTime(60_000);
    expect(refresh).not.toHaveBeenCalled();
  });
});
